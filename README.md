# didactic-lemon-fiesta

A TCP Relay that uses secret-based matchmacking to connect two clients and relay data between them.

<!-- launch relay server -->
ROLE=server PORT=8080 go run main.go

<!-- launch test client A -->
ROLE=client SERVER_URL=localhost:8080 SECRET=todaysSecret2 go run main.go

<!-- launch test client B -->
ROLE=client SERVER_URL=localhost:8080 SECRET=todaysSecret2 FILE=/Users/peterbishop/Development/notes.md go run main.go