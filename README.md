# ISP API Gateway
A cloud-native, high-performance Go microservice that bridges a billing database with a MikroTik ISP network. It handles database state and instantly triggers RADIUS Packets of Disconnect (PoD) to kick unpaying users off the network.

## Tech Stack
* **Go:** REST API and background process execution.
* **PostgreSQL:** Stores `radcheck` and `radusergroup` profiles.
* **FreeRADIUS Utils:** Uses `radclient` for dynamic CoA/PoD control.

## How to Run Locally
Ensure you have Docker installed, then run:
`docker compose up --build -d`

The API will be available at `http://localhost:8080`.

## Example API Request
Suspend a user instantly:

`curl -X POST http://localhost:8080/api/suspend - H "Content-Type: application/json" -d'{"username": "johndoe"}'`