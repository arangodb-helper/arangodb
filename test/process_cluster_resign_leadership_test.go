//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Tomasz Mielech
//

package test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb/go-driver"
	"github.com/pkg/errors"
)

// TestProcessClusterResignLeadership starts a master starter, followed by 2 slave starters.
// It closes the starter where the leader of the shard resides and check whether new leader of the shard is elected.
func TestProcessClusterResignLeadership(t *testing.T) {
	log := GetLogger(t)

	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER} "+createEnvironmentStarterOptions())
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave2.Close()

	starterEndpoints := []string{
		insecureStarterEndpoint(0 * portIncrement),
		insecureStarterEndpoint(1 * portIncrement),
		insecureStarterEndpoint(2 * portIncrement),
	}
	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		for _, endpoint := range starterEndpoints {
			testCluster(t, endpoint, false)
		}
	}

	auth := driver.BasicAuthentication("root", "")
	starterEndpointForCoordinator := insecureStarterEndpoint(1 * portIncrement)
	coordinatorClient, err := CreateClient(t, starterEndpointForCoordinator, client.ServerTypeCoordinator, auth)
	if err != nil {
		t.Fatal(err.Error())
	}

	databaseName := "_system"
	collectionName := "test"

	WaitUntilServiceReadyAPI(t, coordinatorClient, func(t *testing.T, ctx context.Context, c driver.Client) error {
		_, err := coordinatorClient.Database(context.Background(), databaseName)
		return err
	}).ExecuteT(t, 15*time.Second, 500*time.Millisecond)

	database, err := coordinatorClient.Database(context.Background(), databaseName)
	if err != nil {
		t.Fatal(err.Error())
	}
	options := &driver.CreateCollectionOptions{
		ReplicationFactor: 2,
		NumberOfShards:    1,
	}
	collection, err := database.CreateCollection(context.Background(), collectionName, options)
	if err != nil {
		t.Fatal(err.Error())
	}

	// create some documents.
	type book struct {
		Title string `json:"name"`
	}
	var documents []interface{}
	for i := 0; i < 1000; i++ {
		documents = append(documents, &book{Title: fmt.Sprintf("Title %d", i)})
	}
	documentsMetaData, _, err := collection.CreateDocuments(context.Background(), documents)
	if err != nil {
		t.Fatal(err.Error())
	}
	keys := documentsMetaData.Keys()

	dbServerLeader, err := getServerIDLeaderForFirstShard(coordinatorClient, database, collectionName)
	if err != nil {
		t.Fatal(err.Error())
	}

	// find the starter for the given DB server ID.
	starterEndpointWithLeader, err := getStarterEndpointByServerID(t, dbServerLeader, starterEndpoints...)
	if err != nil {
		t.Fatal(err.Error())
	}

	if starterEndpointWithLeader == starterEndpointForCoordinator {
		// The starter with current connection to the coordinator will be closed so the
		// new coordinator connection must be established for the future requests.
		for _, endpoint := range starterEndpoints {
			if endpoint == starterEndpointWithLeader {
				continue
			}

			coordinatorClient, err = CreateClient(t, endpoint, client.ServerTypeCoordinator, auth)
			if err != nil {
				t.Fatal(err.Error())
			}
			database, err = coordinatorClient.Database(context.Background(), databaseName)
			if err != nil {
				t.Fatal(err.Error())
			}
			collection, err = database.Collection(context.Background(), collectionName)
			if err != nil {
				t.Fatal(err.Error())
			}
			break
		}
	}

	// read books during the shutdown of the one starter.
	var errRead error
	ctxShutdown, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		var oneBook book
		for {
			for _, key := range keys {
				if _, errRead = collection.ReadDocument(context.Background(), key, &oneBook); errRead != nil {
					return
				}
			}

			if _, ok := <-ctxShutdown.Done(); !ok {
				return
			}
		}
	}()

	waitForCallFunction(t, ShutdownStarterCall(starterEndpointWithLeader))

	cancel()
	wg.Wait()
	if errRead != nil {
		t.Logf("Reading documents: %s", errRead.Error())
	}

	log.Log("Waiting for shutdown of services")

	NewTimeoutFunc(func() error {
		// check new leader of the shard.
		newDBServerLeader, err := getServerIDLeaderForFirstShard(coordinatorClient, database, collectionName)
		if err != nil {
			log.Log("Error while fetching shard details: %s", err.Error())
			return nil
		}

		if dbServerLeader == newDBServerLeader {
			log.Log("Shard leader is on same server %s", dbServerLeader)
			return nil
		}

		return NewInterrupt()
	}).ExecuteT(t, time.Minute, 500*time.Millisecond)

	// close the rest of the starters.
	for _, endpoint := range starterEndpoints {
		if endpoint == starterEndpointWithLeader {
			continue
		}
		waitForCallFunction(t,
			ShutdownStarterCall(endpoint))
	}
}

// getStarterEndpointByServerID gets starter endpoint for the given server ID.
func getStarterEndpointByServerID(t *testing.T, serverID driver.ServerID, startersEndpoints ...string) (string, error) {
	for _, endpoint := range startersEndpoints {
		auth := driver.BasicAuthentication("root", "")
		dbServerClient, err := CreateClient(t, endpoint, client.ServerTypeDBServer, auth)
		if err != nil {
			return "", errors.Wrap(err, "CreateClient")
		}

		ID, err := dbServerClient.ServerID(context.Background())
		if err != nil {
			return "", errors.Wrap(err, "ServerID")
		}

		if ID == string(serverID) {
			return endpoint, nil
		}
	}

	return "", errors.New("not found")
}

// getShardsForCollection returns shards for the given collection name.
func getShardsForCollection(client driver.Client, database driver.Database,
	collectionName string) (map[driver.ShardID][]driver.ServerID, error) {
	cluster, err := client.Cluster(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "Cluster")
	}

	inventory, err := cluster.DatabaseInventory(context.Background(), database)
	if err != nil {
		return nil, err
	}

	for _, c := range inventory.Collections {
		if c.Parameters.Name == collectionName {
			return c.Parameters.Shards, nil
		}
	}

	return nil, errors.Errorf("there are no shards for the collection %s", collectionName)
}

//getServerIDLeaderForFirstShard returns server ID of the leader shard.
func getServerIDLeaderForFirstShard(client driver.Client, database driver.Database,
	collectionName string) (driver.ServerID, error) {
	newShards, err := getShardsForCollection(client, database, collectionName)
	if err != nil {
		return "", errors.Wrap(err, "getShardsForCollection")
	}

	for _, dbServers := range newShards {
		// first DB server is the leader.
		if len(dbServers) > 0 {
			return dbServers[0], nil
		}
		break
	}

	return "", errors.Errorf("can not find DB server leader for a collection '%s'", collectionName)
}
