package main

import (
	"github.com/radicelle/kraal/connectors/hubspot"
	"github.com/radicelle/kraal/pkg/sdk"
)

func main() {
	connector := hubspot.NewConnector()
	sdk.Main(connector)
}
