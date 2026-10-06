package main

import (
	"context",
	"database/sql",
	"encoding/json",
	"fmt",
	"log",
	"net/http",
	"os",
	"os/exec",
	"time"

	- "github.com/lib/pq"
)

type AppContext struct {
	DB *sql.DB
	RouterIP string
	RadiusSecret string
}

type SubscriberRequest struct {
	Username string `json:"username"`
	Group string `json:"group"`
}

func main(){
	// Load config from env variables
	dbURL := os.Getenv("DATABASE_URL")
	routerIP := os.Getenv("ROUTER_IP")
	radiusSecret := os.Getenv("RADIUS_SECRET")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Connect to PostgreSQL
	db, err := sql.Open("postgres", dbURL)
	if err := db.Ping(); err != nil {
		log.Fatalf("DB Unreachable: %v", err)
	}
	log.Println("Connect to PostgreSQL")

	app := &AppContext{
		DB: db,
		RouterIP: routerIP,
		RadiusSecret: radiusSecret,
	}

	// Set up REST Routes
	mux := http.NewServerMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request)) {
		w.Write([]byte("ISP API Gateway is Online\n"))
	}
	mux.HandleFunc("POST /api/suspend", app.handleSuspendUser)
	
	log.Printf("Starting ISP API on port %s", port)
	http.ListenAndServe(":"+port, mux)
}

// handleSuspendUser updates Postgres and fires the RADIUS disconnect
func (app *AppContext) handleSuspendUser(w http.ResponseWriter, r *http.Request) {
	var req SubscriberRequest
	json.NewDecoder(r.Body).Decode(&req)

	// 1. Update Database
	_, err := app.DB.Exec("UPDATE radusergroup SET groupname = 'Suspended_Users' WHERE username = $1", req.Username)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 2. Fire Packet of Disconnect via shell
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "radclient", fmt.Sprintf("%s:3799", app.RouterIP), "disconnect", app.RadiusSecret)
	cmd.Stdin = fmt.StringReader(fmt.Sprintf("User-Name=%s\n", req.Username))

	if err := cmd.Run(); err !=nil {
		log.Printf("Failed to disconnect %s: %v", req.Username, err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w.Encode(map[string]string{"status": "success", "user":req.Username, "action": "suspended"}))
}