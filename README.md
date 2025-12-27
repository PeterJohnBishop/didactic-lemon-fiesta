# didactic-lemon-fiesta

A basic relay server that uses binary protocol data transfer over TCP between multiple clients.

<!-- launch relay server -->
ROLE=server SERVER_PORT=:8080 go run main.go

<!-- launch test client A -->
ROLE=client CLIENT_ID=ClientA SERVER_URL=localhost:8080 TARGET_ID=ClientB go run main.go

<!-- launch test client B -->
ROLE=client CLIENT_ID=ClientB SERVER_URL=localhost:8080 TARGET_ID=ClientA FILE=/Users/peterbishop/Development/notes.md go run main.go