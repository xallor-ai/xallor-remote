package releasetrust

import (
	"bytes"
	"encoding/json"
)

const maxVerificationOutput = 2 * 1024 * 1024

// Drain excess output without retaining it or blocking the verifier. An
// oversized response cannot authenticate anything, even if its prefix parses.
type proofOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *proofOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxVerificationOutput - b.buffer.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *proofOutput) matches(expected string) bool {
	if b.overflow {
		return false
	}
	var results []struct {
		VerificationResult struct {
			Statement struct {
				Subject []struct {
					Digest struct {
						SHA256 string `json:"sha256"`
					} `json:"digest"`
				} `json:"subject"`
			} `json:"statement"`
		} `json:"verificationResult"`
	}
	if json.Unmarshal(b.buffer.Bytes(), &results) != nil {
		return false
	}
	for _, result := range results {
		for _, subject := range result.VerificationResult.Statement.Subject {
			if subject.Digest.SHA256 == expected {
				return true
			}
		}
	}
	return false
}
