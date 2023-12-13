package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/arangodb-helper/arangodb/service"
	"github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
	"github.com/arangodb/go-driver/http"
	"github.com/arangodb/go-driver/jwt"
)

const (
	projectID = "magency"
)

var (
	agencyKeyStarter            = []string{"arangodb-helper"}
	agencyKeyUpgradePlanEntries = []string{"arangodb-helper", "arangodb", "upgrade-plan", "entries"}
)

var (
	cmdMain = &cobra.Command{
		Use:   projectID,
		Short: "Edit Arangodb Agency state. Use on your own risk",
		Run:   cmdMainRun,
	}

	cmdRead = &cobra.Command{
		Use:   "read",
		Short: "Print upgrade plan entries",
		Run:   cmdReadRun,
	}
	cmdReadStarter = &cobra.Command{
		Use:   "read-starter",
		Short: "Print whole starter state object",
		Run:   cmdReadStarterRun,
	}

	cmdFix = &cobra.Command{
		Use:   "fix",
		Short: "Fix upgrade plan",
		Run:   cmdFixRun,
		PreRun: func(cmd *cobra.Command, args []string) {
			if len(opts.endpoints) == 0 {
				panic("endpoints are not specified")
				return
			}
		},
	}

	opts struct {
		jwt       string
		endpoints []string
	}
)

func init() {
	cmdMain.AddCommand(cmdRead)
	cmdMain.AddCommand(cmdReadStarter)
	cmdMain.AddCommand(cmdFix)

	fs := cmdMain.PersistentFlags()
	fs.StringVar(&opts.jwt, "jwt", "", "auth JWT")
	fs.StringArrayVar(&opts.endpoints, "endpoint", []string{}, "list of agency endpoints")
}

func main() {
	if err := cmdMain.Execute(); err != nil {
		os.Exit(1)
	}
}

func printDump(msg string, obj interface{}) {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(fmt.Sprintf("Can't print %s: %s", msg, err))
	}
	log.Printf("%s: %s", msg, string(b))
}

func cmdReadRun(cmd *cobra.Command, args []string) {
	c := prepareClient()

	entries := readEntries(c)
	printDump("curr entries", entries)
}
func cmdReadStarterRun(cmd *cobra.Command, args []string) {
	c := prepareClient()

	ctx := context.Background()
	var obj interface{}
	err := c.ReadKey(ctx, agencyKeyStarter, &obj)
	if err != nil {
		panic(err)

	}
	printDump("Starter state:", obj)
}

func cmdFixRun(cmd *cobra.Command, args []string) {
	c := prepareClient()

	currEntries := readEntries(c)
	printDump("curr entries", currEntries)
	if len(currEntries) == 0 {
		log.Println("No upgrade plan entries. No fix needed")
		return
	}

	var newEntries []service.UpgradePlanEntry
	for _, entry := range currEntries {
		if entry.Type == service.UpgradeEntryTypeSyncMaster || entry.Type == service.UpgradeEntryTypeSyncWorker {
			log.Printf("Filtering out entry: %+v\n", entry)
			continue
		}
		newEntries = append(newEntries, entry)
	}
	printDump("New entries", newEntries)

	tx := agency.NewTransaction(projectID, agency.TransactionOptions{})
	tx.AddKey(agency.NewKeySet(agencyKeyUpgradePlanEntries, newEntries, 0))
	err := tx.AddCondition(agencyKeyUpgradePlanEntries, agency.NewConditionIsArray(true))
	if err != nil {
		panic(err)
		return
	}

	printDump("WriteTransaction", tx)

	ctx := context.Background()
	err = c.WriteTransaction(ctx, tx)
	if err != nil {
		panic(err)
		return
	}
	log.Println("done")
}

func prepareClient() agency.Agency {
	conn, err := agency.NewAgencyConnection(http.ConnectionConfig{
		Endpoints: opts.endpoints,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
	if err != nil {
		panic(err)
	}

	jwtBearer, err := jwt.CreateArangodJwtAuthorizationHeader(opts.jwt, projectID)
	if err != nil {
		panic(err)
	}
	c, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.RawAuthentication(jwtBearer),
	})

	agencyClient, err := agency.NewAgency(c.Connection())
	if err != nil {
		panic(err)
	}
	return agencyClient
}

func readEntries(agencyClient agency.Agency) []service.UpgradePlanEntry {
	ctx := context.Background()
	var currEntries []service.UpgradePlanEntry
	err := agencyClient.ReadKey(ctx, agencyKeyUpgradePlanEntries, &currEntries)
	if err != nil {
		panic(err)

	}
	return currEntries
}

func cmdMainRun(cmd *cobra.Command, args []string) {
	cmd.Usage()
}
