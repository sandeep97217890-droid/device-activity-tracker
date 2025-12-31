# Device Activity Tracker

<p align="center">WhatsApp Activity Tracker via RTT Analysis - Pure Go Implementation</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go"/>
  <img src="https://img.shields.io/badge/WhatsApp-whatsmeow-25D366?style=flat&logo=whatsapp&logoColor=white" alt="WhatsApp"/>
  <img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License MIT"/>
</p>

> ⚠️ **DISCLAIMER**: Proof-of-concept for educational and security research purposes only. Demonstrates privacy vulnerabilities in WhatsApp.

## Overview

This project implements the research from the paper **"Careless Whisper: Exploiting Silent Delivery Receipts to Monitor Users on Mobile Instant Messengers"** by Gabriel K. Gegenhuber, Maximilian Günther, Markus Maier, Aljosha Judmayer, Florian Holzbauer, Philipp É. Frenzel, and Johanna Ullrich (University of Vienna & SBA Research).

**What it does:** By measuring Round-Trip Time (RTT) of WhatsApp message delivery receipts, this tool can detect:
- When a user is actively using their device (low RTT)
- When the device is in standby/idle mode (higher RTT)
- Potential location changes (mobile data vs. WiFi)
- Activity patterns over time

**Security implications:** This demonstrates a significant privacy vulnerability in messaging apps that can be exploited for surveillance.

## Example

![WhatsApp Activity Tracker Interface](example.png)

## Requirements

- **Go 1.21** or higher
- WhatsApp account
- Terminal (command-line interface)

## Installation

```bash
# Clone repository
git clone https://github.com/sandeep97217890-droid/device-activity-tracker.git
cd device-activity-tracker

# Build the application
go build -o tracker main.go
```

## Usage

```bash
# Run the tracker
./tracker
```

**Steps:**
1. The application will display a QR code in your terminal
2. Scan the QR code with WhatsApp (Linked Devices)
3. Once connected, enter the target phone number (with country code, e.g., `14155551234`)
4. The tracker will start monitoring device activity in real-time

**Example Output:**

```
╔════════════════════════════════════════════════════════════════╗
║ 🟡 Device Status Update - 09:41:51                             ║
╠════════════════════════════════════════════════════════════════╣
║ JID:        14155551234@s.whatsapp.net                         ║
║ Status:     Standby                                            ║
║ RTT:        1104ms                                             ║
║ Avg (3):    1161ms                                             ║
║ Median:     1195ms                                             ║
║ Threshold:  1075ms                                             ║
╚════════════════════════════════════════════════════════════════╝
```

**Status Indicators:**
- **🟢 Online**: Device is actively being used (RTT below threshold)
- **🟡 Standby**: Device is idle/locked (RTT above threshold)
- **🔴 OFFLINE**: Device is offline or unreachable (no CLIENT ACK received)

## How It Works

The tracker sends probe messages and measures the Round-Trip Time (RTT) to detect device activity.

### Probe Method

The tool uses a **silent delete probe** method:
- Sends a "delete" request for a non-existent message ID
- Completely covert - no notification or visible trace
- Measures time until delivery receipt is received

### Detection Logic

The tracker implements a sophisticated state detection algorithm with the following features:

**RTT Measurement:**
- Start time is recorded BEFORE sending the probe message for accurate RTT calculation
- Multiple message ID prefixes are used to avoid detection patterns
- Probe intervals are randomized (2000-2100ms) to appear more natural

**State Determination:**
- Uses a moving average of the last 3 RTT measurements
- Calculates a dynamic threshold at 90% of the global median RTT
- Values below threshold indicate active usage ("Online")
- Values above threshold indicate standby mode ("Standby")
- No response within 10 seconds marks device as "OFFLINE"

**State Stability (Hysteresis):**
- Requires at least 3 measurements before determining Online/Standby state
- Requires at least 5 global measurements before leaving "Calibrating..." state
- Implements 6-second hysteresis before state transitions
- Prevents rapid oscillation between states
- Only displays updates when state actually changes, reducing console noise

**State Transitions:**
- OFFLINE → Online/Standby: Only when valid measurements received
- Online ↔ Standby: Only after 6 seconds in current state
- Prevents flickering and ensures stable status reporting

## Common Issues

- **Not Connecting to WhatsApp**: Delete the `whatsapp.db` file and re-scan the QR code.
- **Build Errors**: Ensure you have Go 1.21 or higher installed: `go version`

## Project Structure

```
device-activity-tracker/
├── main.go            # Main application with WhatsApp RTT analysis logic
├── go.mod             # Go module dependencies
├── go.sum             # Dependency checksums
└── README.md          # This file
```

## Technology Stack

- **Language**: Go (Golang)
- **WhatsApp Library**: [whatsmeow](https://github.com/tulir/whatsmeow) - Pure Go WhatsApp Web API
- **Database**: SQLite (for session storage)
- **QR Code**: Terminal-based QR code display

## How to Protect Yourself

The most effective mitigation is to enable "Block unknown account messages" in WhatsApp under Settings → Privacy → Advanced.

This setting may reduce an attacker's ability to spam probe reactions from unknown numbers, because WhatsApp blocks high-volume messages from unknown accounts. However, WhatsApp does not disclose what "high volume" means, so this does not fully prevent an attacker from sending a significant number of probe reactions before rate-limiting kicks in.

Disabling read receipts helps with regular messages but does not protect against this specific attack. As of December 2025, this vulnerability remains exploitable in WhatsApp.

## Ethical & Legal Considerations

⚠️ For research and educational purposes only. Never track people without explicit consent - this may violate privacy laws. Authentication data (`whatsapp.db`) is stored locally and must never be committed to version control.

## Citation

Based on research by Gegenhuber et al., University of Vienna & SBA Research:

```bibtex
@inproceedings{gegenhuber2024careless,
  title={Careless Whisper: Exploiting Silent Delivery Receipts to Monitor Users on Mobile Instant Messengers},
  author={Gegenhuber, Gabriel K. and G{\"u}nther, Maximilian and Maier, Markus and Judmayer, Aljosha and Holzbauer, Florian and Frenzel, Philipp {\'E}. and Ullrich, Johanna},
  year={2024},
  organization={University of Vienna, SBA Research}
}
```

## License

MIT License - See LICENSE file.

Built with [whatsmeow](https://github.com/tulir/whatsmeow) - Pure Go WhatsApp Web API

---

**Use responsibly. This tool demonstrates real security vulnerabilities that affect millions of users.**
