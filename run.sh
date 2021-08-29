#!/bin/bash

export PGHOST=localhost
export PGPORT=5532
export PGUSER=gorm
export PGDATABASE=gorm
export PGPASSWORD=gorm
export PGSSLMODE=disable
export SUFX="-G1"

case $1 in
	"dbconnect")
		psql -h localhost --port $PGPORT -d $PGDATABASE -U $PGUSER
	;;
	"dbexec")
		psql -h localhost --port $PGPORT -d $PGDATABASE -U $PGUSER -c "$2"
	;;
	"dbrun")
		psql -h localhost --port $PGPORT -d $PGDATABASE -U $PGUSER < "$2"
	;;
    "run")
    ./bin/orders
esac

