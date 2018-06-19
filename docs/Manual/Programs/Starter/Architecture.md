please explain starter architure at a general high level, in particular, highlight the following:

- the starter will create the arangod processes, and restart them if they die
- explain that the starter has its own version and that this is different from the ArangoDB version, and can be checked using --version option
- describe that the starter starts a cluster (for itself) and clarify to not confuse this with the arangodb cluster
- clarify what master and slaves means in terms of the starter - ideally let's rename to "starter primary" and "starter secondary" to avoid confusion with master and slaves terms, that we use in Arangod
- cite that a json file is created - and suggest to not edit this manually
- when running cluster, the first 3 machines have a A, C, D and the others only a C and D, but this can be changed using options
- cite that to add more options, one has to purge old data directory, as new options otherwise will not take effect OR edit directly the configuration files of the services 
