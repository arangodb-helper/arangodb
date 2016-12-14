#!/bin/bash

CURL="curl --insecure -s -f -X"
testServer() {
  PORT=$1
  while true ; do
    $CURL GET http://127.0.0.1:$PORT/_api/version > /dev/null 2>&1
    if [ "$?" != "0" ] ; then
      echo Server on port $PORT does not answer yet.
    else
      echo Server on port $PORT is ready for business.
      break
    fi
    sleep 1
  done
}

rm -rf a b c a.log b.log c.log
mkdir a b c
arangodb/arangodb --dataDir a >a.log &
PID1=$!
sleep 1
arangodb/arangodb --dataDir b --join localhost >b.log &
PID2=$!
sleep 1
arangodb/arangodb --dataDir c --join localhost >c.log &
PID3=$!

testServer 4001
testServer 4002
testServer 4003
testServer 8629
testServer 8630
testServer 8631
testServer 8530
testServer 8531
testServer 8532

$CURL POST http://localhost:8530/_api/collection -d '{"name":"c","replicationFactor":2,"numberOfShards":3}' >/dev/null 2>&1
if [ "$?" != "0" ] ; then
  echo Could not create collection!
  kill -2 ${PID1} ${PID2} ${PID3}
  wait
  exit 1
fi

$CURL POST http://localhost:8530/_api/collection/c -d '{"name":"Max"}' >/dev/null 2>&1
if [ "$?" != "0" ] ; then
  echo Could not create document!
  kill -2 ${PID1} ${PID2} ${PID3}
  wait
  exit 1
fi

$CURL DELETE http://localhost:8530/_api/collection/c >/dev/null 2>&1
if [ "$?" != "0" ] ; then
  echo Could not drop collection!
  kill -2 ${PID1} ${PID2} ${PID3}
  wait
  exit 1
fi

echo Cluster seems to work... Sleeping for 15s...
sleep 15

kill -2 ${PID1} ${PID2} ${PID3}
wait

echo Test successful
rm -rf a b c a.log b.log c.log
