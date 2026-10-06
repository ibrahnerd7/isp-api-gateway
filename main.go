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