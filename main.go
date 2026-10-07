package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type AppContext struct {
	DB           *sql.DB
	RouterIP     string
	RadiusSecret string
}

type SubscriberRequest struct {
	Username string `json:"username"`
	Group    string `json:"group"`
}

// MpesaCallback matches the Safaricom STK Push JSON structure
type MpesaCallback struct {
	Body struct {
		StkCallback struct {
			MerchantRequestID string `json:"MerchantRequestID"`
			CheckoutRequestID string `json:"CheckoutRequestID"`
			ResultCode int `json:"ResultCode"`
			ResultDesc string `json:"ResultDesc"`
			CallbackMetadata struct {
				Item []struct {
					Name string `json:"Name"`
					Value interface{} `json:"Value"`
				} `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	}`json: "Body"`
}

func main() {
	// Load config from env variables
	dbURL := os.Getenv("DATABASE_URL")
	routerIP := os.Getenv("ROUTER_IP")
	radiusSecret := os.Getenv("RADIUS_SECRET")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Connect to PostgreSQL with a Retry loop
	var db *sql.DB
	var err error
	
	for i := 1; i <= 5; i++ {
		db, err = sql.Open("postgres", dbURL)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break // Success! Exit the loop
			}
		}
		log.Printf("Database not ready (Attemp %d/5). Retrying in 3 seconds...", i)
		time.Sleep(3 * time.Second)

	}

	if err != nil {
		log.Fatalf("Failed to open DB connection: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("DB Unreachable: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	app := &AppContext{
		DB:           db,
		RouterIP:     routerIP,
		RadiusSecret: radiusSecret,
	}

	// Set up REST Routes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ISP API Gateway is Online\n"))
	})
	mux.HandleFunc("POST /api/suspend", app.handleSuspendUser)
	mux.HandleFunc("POST /api/webhooks/mpesa", app.handleMpesaWebhook)


	log.Printf("Starting ISP API on port %s", port)
	http.ListenAndServe(":"+port, mux)
}

// handleSuspendUser updates Postgres and fires the RADIUS disconnect
func (app *AppContext) handleSuspendUser(w http.ResponseWriter, r *http.Request) {
	var req SubscriberRequest
	
	// Safely decode the JSON payload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

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
	cmd.Stdin = strings.NewReader(fmt.Sprintf("User-Name=%s\n", req.Username))
	
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to disconnect %s: %v", req.Username, err)
	}

	// 3. Return JSON Response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"user":   req.Username,
		"action": "suspended",
	})
}

func (app *AppContext) handleMpesaWebhook(w http.ResponseWriter, r *http.Request) {
	var callback MpesaCallback
	
	if err := json.NewDecoder(r.Body).Decode(&callback); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	stk := callback.Body.StkCallback

	// ResultCode 0 means the customer successfully paid
	if stk.ResultCode == 0 {
		var phoneNumber string
		var amount float64

		// Extract payment details from the Metadata array
		for _, item := range stk.CallbackMetadata.Item {
			if item.Name == "PhoneNumber" {
				// Safaricom sends numbers as floats in JSON, convert safely
				phoneNumber = fmt.Sprintf("%.0f", item.Value.(float64))
			}
			if item.Name == "Amount" {
				amount = item.Value.(float64)
			}
		}

		log.Printf("Payment Received: Ksh %.2f from %s", amount, phoneNumber)

		// 1. Unsuspend the user in PostgreSQL
		// (Assuming the phone number is used as the username for simplicity)
		_, err := app.DB.Exec("UPDATE radusergroup SET groupname = 'Gold_Plan' WHERE username = $1", phoneNumber)
		if err != nil {
			log.Printf("Database error updating user %s: %v", phoneNumber, err)
		}

		// 2. Drop the dead/suspended session so the router forces a re-auth 
		// and applies the new 'Gold_Plan' speed profile immediately
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		
		cmd := exec.CommandContext(ctx, "radclient", fmt.Sprintf("%s:3799", app.RouterIP), "disconnect", app.RadiusSecret)
		cmd.Stdin = strings.NewReader(fmt.Sprintf("User-Name=%s\n", phoneNumber))
		
		if err := cmd.Run(); err == nil {
			log.Printf("Successfully unsuspended and bounced session for %s", phoneNumber)
		}
	} else {
		// ResultCode != 0 means cancelled, failed, or timed out
		log.Printf("Failed payment attempt: %s", stk.ResultDesc)
	}

	// Safaricom expects a simple success acknowledgment so they stop retrying
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"ResultCode": "0",
		"ResultDesc": "Accepted",
	})
}