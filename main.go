package main

import (
	"flag"
	"os"

	elastigo "github.com/jacqui/elastigo/lib"
	"github.com/nytlabs/hive/hive"
)

var (
	port     = flag.String("port", "8080", "hive port")
	esDomain = flag.String("esDomain", "localhost", "elasticsearch domain")
	esPort   = flag.String("esPort", "9200", "elasticsearch port")
	index    = flag.String("index", "hive", "elasticsearch index name")
)

func main() {
	flag.Parse()

	s := hive.NewServer()

	// what port should the hive server run on
	s.Port = *port

	// allow overriding elasticsearch index name
	// this is useful for testing
	s.Index = *index

	conn := elastigo.NewConn()

	// EnvVar set via etcd/fleet
	esDomainEnv := os.Getenv("ELASTICSEARCH_DOMAIN")
	esPortEnv := os.Getenv("ELASTICSEARCH_PORT")
	if esDomainEnv != "" {
		conn.Domain = esDomainEnv
	} else {
		conn.Domain = *esDomain
	}

	if esPortEnv != "" {
		conn.Port = esPortEnv
	} else {
		conn.Port = *esPort
	}

	s.EsConn = *conn

	s.Run()
}
