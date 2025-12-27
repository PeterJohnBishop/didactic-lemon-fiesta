package main

import (
	"didactic-lemon-fiesta/relayclient"
	"didactic-lemon-fiesta/server"
	"fmt"
	"os"
)

func main() {
	role := os.Getenv("ROLE")
	switch role {
	case "server":
		fmt.Println("Starting server...")
		server.LaunchRelayServer()
	case "client":
		fmt.Println("Starting relay client...")
		relayclient.LaunchRelayClient()
	default:
		fmt.Println("Please set the ROLE environment variable to 'server' or 'client'.")
	}
}
