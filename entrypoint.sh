#!/bin/sh

go build
./fleet-provider -nodesrv nodeserv:9990 -host fleet_provider
