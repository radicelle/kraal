package main

import (
	"github.com/radicelle/kraal/connectors/postgres"
	"github.com/radicelle/kraal/pkg/sdk"
)

func main() {
	connector := postgres.NewConnector()
	sdk.Main(connector)
}
