# ArangoDB Starter Design

## Terminology 

- `Bootstrap` the process of creating a cluster (or single server) from scratch.
- `Relaunch` the process to restarting a cluster from an existing (persisted) state.
- `Cluster Configuration` a network of starters that, when all running, form a cluster / single server.
- `Peer` single starter in a cluster configuration.
- `Database Server` an instance of `arangod` configured as single server, agent, dbserver or coordinator.
- `Service` the golang implementation of the starter processes.

## Startup 

When the starter starts, it reads all command line options and performs some simple checks for consistency.
It then tries to read a persisted starter configuration `setup.json`. 
If that is found and valid, the starter continues with the [Relaunch](#relaunch) process.
If that is not found or completely outdated, the starter continues with the [Bootstrap](#bootstrap) process.

## Bootstrap 

This chapter describes the process taken by one or more starters to create a cluster from scratch.
Once the bootstrap process has completed a valid cluster configuration exists and it known by 
all involved starters. Database server can now be started (but have not yet been started).

When bootstrapping, 1 starter acts as the master and all other starters act as slave.

### Master

When the master starts a bootstrapping process it performs the following steps.

- Create an empty cluster configuration and add itself as peer.
- Start local slaves (when bootstrap configuration tells it to do so).
- Receive join requests (POST `/hello`) from slaves.

When the cluster configuration has reached a state where enough peers have been added to create 
an agency of the intended size, the master continues to the [Running](#running_state).

### Slave 

When the slave starts a bootstrapping process it performs the following steps.

- Send a join request to the master (repeats until success response is received).
- Frequently ask master for current cluster configuration

When the cluster configuration has reached a state where enough peers have been added to create 
an agency of the intended size, the slave continues to the [Running](#running_state).

## Relaunch 

This chapter describes the process taken by one or more starters to restart a cluster from an existing persistent state.

When relaunching all starters are considered the same. There is no master/slave differentiation.

Each starter loads the last known cluster configuration from `setup.json`. 
The information in this setup file is sufficient to:
- know all other starts in the cluster
- know the size of the agency 
- know the mode in which the starter will operate (single|cluster)
- know if the starter should start local slaves 
- know enough about the authentication settings of the database server to be able to contact them successfully

All this information is loaded into the fields of the `Service` and then
the starter continues to the [Running](#running_state).

## Running state 

This chapter describes the process taken by the starters after they have bootstrapped or relaunched and a cluster configuration exists.

In the running state, the `Service` will keep starting database servers until a stop of the service has been requested.
The type of database servers that will be starter is found in the cluster 
configuration for this specific starter.
For each type of database server that needs to be started, the starter 
performs a loop that:
- check if a database server is already running on the intended port, if so, adopt that process
- if no such process it found, it starts the database server 
- wait for the database server to terminate
- show error & logs when database server terminates to quickly or too often.

TODO 
- Keep distributing current cluster configuration (design & implement)

## Cluster reconfiguration 

### Adding new peers  

TODO 

### Removing peers 

TODO 
