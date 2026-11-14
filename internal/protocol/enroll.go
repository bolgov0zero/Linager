package protocol

import (
	"encoding/base64"
	"strconv"
	"time"
)

// EnrollRequest is sent by an agent that wants to be paired.
type EnrollRequest struct {
	Pubkey       []byte `json:"pubkey"`
	Hostname     string `json:"hostname"`
	Address      string `json:"address"`
	OS           string `json:"os"`
	AgentVersion string `json:"agent_version"`
	Protocol     int    `json:"protocol"`
	TS           int64  `json:"ts"`
	Sig          []byte `json:"sig"`
}

// EnrollSigningBytes is what the agent signs to prove possession of its key.
func EnrollSigningBytes(pub []byte, hostname string, ts int64) []byte {
	return []byte("linager-enroll\n" + base64.StdEncoding.EncodeToString(pub) + "\n" + hostname + "\n" + itoa64(ts))
}

// PollSigningBytes is what the agent signs when polling the status of its request.
func PollSigningBytes(id string, ts int64) []byte {
	return []byte("linager-poll\n" + id + "\n" + itoa64(ts))
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// EnrollResponse is returned after a request is registered.
type EnrollResponse struct {
	ID        string    `json:"id"`
	Nonce     []byte    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

// EnrollStatus is returned when polling.
type EnrollStatus struct {
	Status    string `json:"status"`
	ServerID  string `json:"server_id,omitempty"`
	CertPEM   string `json:"cert_pem,omitempty"`
	CAPEM     string `json:"ca_pem,omitempty"`
	CmdPubkey []byte `json:"cmd_pubkey,omitempty"`
}
