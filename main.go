package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-colorable"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	waProto "go.mau.fi/whatsmeow/binary/proto"
)

// DeviceMetrics tracks RTT measurements and device state
type DeviceMetrics struct {
	RTTHistory  []int64
	RecentRTTs  []int64
	State       string
	LastRTT     int64
	LastUpdate  time.Time
}

// WhatsAppTracker monitors user activity using RTT-based analysis
type WhatsAppTracker struct {
	client              *whatsmeow.Client
	targetJID           types.JID
	isTracking          bool
	deviceMetrics       map[string]*DeviceMetrics
	globalRTTHistory    []int64
	probeStartTimes     map[string]time.Time
	stopChan            chan struct{}
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║         Device Activity Tracker - WhatsApp Edition           ║")
	fmt.Println("║           RTT-Based Activity Analysis (Go/whatsmeow)         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("⚠️  DISCLAIMER: For educational and security research only.")
	fmt.Println()

	// Set up database for WhatsApp session
	dbLog := waLog.Stdout("Database", "ERROR", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:whatsapp.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(fmt.Errorf("failed to create database: %v", err))
	}

	// Get first device or create new one
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(fmt.Errorf("failed to get device: %v", err))
	}

	clientLog := waLog.Stdout("Client", "ERROR", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Connect to WhatsApp
	if client.Store.ID == nil {
		// No ID stored, need to scan QR code
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(fmt.Errorf("failed to connect: %v", err))
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("📱 Scan this QR code with WhatsApp:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			} else {
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		// Already logged in, just connect
		err = client.Connect()
		if err != nil {
			panic(fmt.Errorf("failed to connect: %v", err))
		}
	}

	fmt.Println("✅ Connected to WhatsApp")
	fmt.Println()

	// Ask for target phone number
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter target phone number (with country code, e.g., 14155551234): ")
	number, _ := reader.ReadString('\n')
	number = strings.TrimSpace(number)
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")
	number = strings.ReplaceAll(number, "+", "")

	if len(number) < 10 {
		fmt.Println("❌ Invalid phone number")
		return
	}

	// Create JID from phone number
	targetJID := types.NewJID(number, types.DefaultUserServer)

	// Check if number is on WhatsApp
	resp, err := client.IsOnWhatsApp(context.Background(), []string{targetJID.String()})
	if err != nil {
		fmt.Printf("❌ Error checking number: %v\n", err)
		return
	}

	if len(resp) == 0 || !resp[0].IsIn {
		fmt.Println("❌ Number not registered on WhatsApp")
		return
	}

	fmt.Printf("✅ Tracking started for %s\n", targetJID.String())
	fmt.Println()

	// Create tracker and start tracking
	tracker := NewWhatsAppTracker(client, targetJID)
	globalTracker = tracker // Set global tracker for receipt handler
	
	// Set up event handler for receipts after tracker is created
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Receipt:
			handleReceipt(v)
		case *events.Message:
			// Ignore regular messages for now
		}
	})
	
	tracker.StartTracking()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\n🛑 Stopping tracker...")
	tracker.StopTracking()
	client.Disconnect()
	fmt.Println("👋 Goodbye!")
}

// NewWhatsAppTracker creates a new tracker instance
func NewWhatsAppTracker(client *whatsmeow.Client, targetJID types.JID) *WhatsAppTracker {
	return &WhatsAppTracker{
		client:           client,
		targetJID:        targetJID,
		deviceMetrics:    make(map[string]*DeviceMetrics),
		globalRTTHistory: make([]int64, 0),
		probeStartTimes:  make(map[string]time.Time),
		stopChan:         make(chan struct{}),
	}
}

// StartTracking begins the tracking loop
func (t *WhatsAppTracker) StartTracking() {
	if t.isTracking {
		return
	}
	t.isTracking = true

	// Subscribe to presence updates
	err := t.client.SubscribePresence(context.Background(), t.targetJID)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not subscribe to presence: %v\n", err)
	}

	// Start probe loop in background
	go t.probeLoop()
}

// StopTracking stops the tracking loop
func (t *WhatsAppTracker) StopTracking() {
	if !t.isTracking {
		return
	}
	t.isTracking = false
	close(t.stopChan)
}

// probeLoop sends periodic probe messages
func (t *WhatsAppTracker) probeLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ticker.C:
			t.sendDeleteProbe()
		}
	}
}

// sendDeleteProbe sends a silent delete probe message
func (t *WhatsAppTracker) sendDeleteProbe() {
	// Generate a random message ID that doesn't exist
	fakeMessageID := fmt.Sprintf("3EB0%s%d", generateRandomString(8), time.Now().UnixNano()%1000000)

	// Create delete message
	deleteMsg := &waProto.Message{
		ProtocolMessage: &waProto.ProtocolMessage{
			Type: waProto.ProtocolMessage_REVOKE.Enum(),
			Key: &waProto.MessageKey{
				RemoteJID: proto.String(t.targetJID.String()),
				FromMe:    proto.Bool(true),
				ID:        proto.String(fakeMessageID),
			},
		},
	}

	startTime := time.Now()
	resp, err := t.client.SendMessage(context.Background(), t.targetJID, deleteMsg)
	if err != nil {
		fmt.Printf("⚠️  Error sending probe: %v\n", err)
		return
	}

	// Store probe start time for RTT calculation
	if resp.ID != "" {
		t.probeStartTimes[resp.ID] = startTime
		
		// Set timeout for offline detection
		go func(msgID string) {
			time.Sleep(10 * time.Second)
			if _, exists := t.probeStartTimes[msgID]; exists {
				// No receipt received - mark as offline
				delete(t.probeStartTimes, msgID)
				t.markDeviceOffline(t.targetJID.String(), 10000)
			}
		}(resp.ID)
	}
}

// handleReceipt is called globally for all receipt events
var globalTracker *WhatsAppTracker

func handleReceipt(receipt *events.Receipt) {
	if globalTracker == nil {
		return
	}

	// Check if this receipt is from our target
	if receipt.Sender.String() != globalTracker.targetJID.String() {
		return
	}

	// Process each message ID in the receipt
	for _, msgID := range receipt.MessageIDs {
		if startTime, exists := globalTracker.probeStartTimes[msgID]; exists {
			rtt := time.Since(startTime).Milliseconds()
			delete(globalTracker.probeStartTimes, msgID)
			globalTracker.addMeasurement(receipt.Sender.String(), rtt)
		}
	}
}

// addMeasurement adds an RTT measurement and updates device state
func (t *WhatsAppTracker) addMeasurement(jid string, rtt int64) {
	// Initialize metrics if needed
	if t.deviceMetrics[jid] == nil {
		t.deviceMetrics[jid] = &DeviceMetrics{
			RTTHistory: make([]int64, 0),
			RecentRTTs: make([]int64, 0),
			State:      "Calibrating...",
			LastRTT:    rtt,
			LastUpdate: time.Now(),
		}
	}

	metrics := t.deviceMetrics[jid]

	// Only add valid measurements
	if rtt <= 5000 {
		// Add to recent RTTs (last 3)
		metrics.RecentRTTs = append(metrics.RecentRTTs, rtt)
		if len(metrics.RecentRTTs) > 3 {
			metrics.RecentRTTs = metrics.RecentRTTs[1:]
		}

		// Add to history (last 2000)
		metrics.RTTHistory = append(metrics.RTTHistory, rtt)
		if len(metrics.RTTHistory) > 2000 {
			metrics.RTTHistory = metrics.RTTHistory[1:]
		}

		// Add to global history
		t.globalRTTHistory = append(t.globalRTTHistory, rtt)
		if len(t.globalRTTHistory) > 2000 {
			t.globalRTTHistory = t.globalRTTHistory[1:]
		}

		metrics.LastRTT = rtt
		metrics.LastUpdate = time.Now()

		t.determineDeviceState(jid)
	}
}

// determineDeviceState calculates device state based on RTT
func (t *WhatsAppTracker) determineDeviceState(jid string) {
	metrics := t.deviceMetrics[jid]
	if metrics == nil {
		return
	}

	// Calculate moving average
	if len(metrics.RecentRTTs) == 0 {
		return
	}

	var sum int64
	for _, rtt := range metrics.RecentRTTs {
		sum += rtt
	}
	movingAvg := float64(sum) / float64(len(metrics.RecentRTTs))

	// Calculate global median and threshold
	if len(t.globalRTTHistory) < 3 {
		metrics.State = "Calibrating..."
		return
	}

	median := calculateMedian(t.globalRTTHistory)
	threshold := float64(median) * 0.9

	if movingAvg < threshold {
		metrics.State = "Online"
	} else {
		metrics.State = "Standby"
	}

	// Display formatted output
	displayDeviceState(jid, metrics.LastRTT, int64(movingAvg), median, int64(threshold), metrics.State)
}

// markDeviceOffline marks a device as offline
func (t *WhatsAppTracker) markDeviceOffline(jid string, timeout int64) {
	if t.deviceMetrics[jid] == nil {
		t.deviceMetrics[jid] = &DeviceMetrics{
			RTTHistory: make([]int64, 0),
			RecentRTTs: make([]int64, 0),
			State:      "OFFLINE",
			LastRTT:    timeout,
			LastUpdate: time.Now(),
		}
	} else {
		metrics := t.deviceMetrics[jid]
		metrics.State = "OFFLINE"
		metrics.LastRTT = timeout
		metrics.LastUpdate = time.Now()
	}

	fmt.Printf("\n🔴 Device %s marked as OFFLINE (no receipt after %dms)\n\n", jid, timeout)
}

// displayDeviceState prints formatted device state
func displayDeviceState(jid string, rtt, avgRtt, median, threshold int64, state string) {
	stateColor := "⚪"
	switch state {
	case "Online":
		stateColor = "🟢"
	case "Standby":
		stateColor = "🟡"
	case "OFFLINE":
		stateColor = "🔴"
	}

	timestamp := time.Now().Format("15:04:05")
	
	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Printf("║ %s Device Status Update - %s%s║\n", stateColor, timestamp, strings.Repeat(" ", 64-len(timestamp)-31))
	fmt.Println("╠════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ JID:        %-50s ║\n", jid)
	fmt.Printf("║ Status:     %-50s ║\n", state)
	fmt.Printf("║ RTT:        %-50s ║\n", fmt.Sprintf("%dms", rtt))
	fmt.Printf("║ Avg (3):    %-50s ║\n", fmt.Sprintf("%dms", avgRtt))
	fmt.Printf("║ Median:     %-50s ║\n", fmt.Sprintf("%dms", median))
	fmt.Printf("║ Threshold:  %-50s ║\n", fmt.Sprintf("%dms", threshold))
	fmt.Println("╚════════════════════════════════════════════════════════════════╝\n")
}

// calculateMedian calculates the median of a slice of int64
func calculateMedian(data []int64) int64 {
	if len(data) == 0 {
		return 0
	}

	// Copy and sort using built-in sort
	sorted := make([]int64, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// generateRandomString generates a random uppercase alphanumeric string
func generateRandomString(length int) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	randomBytes := make([]byte, length)
	
	// Use crypto/rand for secure randomness
	_, err := rand.Read(randomBytes)
	if err != nil {
		// Fallback to timestamp-based if crypto/rand fails
		for i := range result {
			result[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		}
		return string(result)
	}
	
	for i := range result {
		result[i] = chars[int(randomBytes[i])%len(chars)]
	}
	return string(result)
}
