package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
)

const (
	None   = "none"
	Plain  = "plain"
	SHA256 = "sha256"
	SHA512 = "sha512"
)

type Credentials struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Algorithm     string `json:"algorithm"`
	ClientCertPem string `json:"clientCertPem"`
	ClientKeyPem  string `json:"clientKeyPem"`
	ServerCaPem   string `json:"serverCaPem"`
}

func UnmarshalCredentials(auth string) (*Credentials, *Xk6KafkaError) {
	creds := &Credentials{
		Algorithm: None,
	}

	err := json.Unmarshal([]byte(auth), &creds)

	if err != nil {
		return nil, NewXk6KafkaError(
			failedUnmarshalCreds, "Unable to unmarshal credentials", err)
	} else {
		return creds, nil
	}
}

func GetDialerFromCreds(creds *Credentials) (*kafkago.Dialer, *Xk6KafkaError) {
	tlsConfig, err := TLSConfig(creds)
	if err != nil && err.Unwrap() != nil {
		return nil, err
	}

	dialer := &kafkago.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		TLS:       tlsConfig,
	}

	if creds.Algorithm == Plain {
		mechanism := plain.Mechanism{
			Username: creds.Username,
			Password: creds.Password,
		}
		dialer.SASLMechanism = mechanism
		return dialer, nil
	} else if creds.Algorithm == SHA256 || creds.Algorithm == SHA512 {
		hashes := make(map[string]scram.Algorithm)
		hashes["sha256"] = scram.SHA256
		hashes["sha512"] = scram.SHA512

		mechanism, err := scram.Mechanism(
			hashes[creds.Algorithm],
			creds.Username,
			creds.Password,
		)
		if err != nil {
			return nil, NewXk6KafkaError(
				failedCreateDialerWithScram, "Unable to create SCRAM mechanism", err)
		}
		dialer.SASLMechanism = mechanism
		return dialer, nil
	}
	return dialer, nil
}

func GetDialerFromAuth(auth string) (*kafkago.Dialer, *Xk6KafkaError) {
	if auth != "" {
		// Parse the auth string
		creds, err := UnmarshalCredentials(auth)
		if err != nil {
			return nil, err
		}

		// Try to create an authenticated dialer from the credentials
		// with TLS enabled if the credentials specify a client cert
		// and key.
		return GetDialerFromCreds(creds)
	} else {
		// Create a normal (unauthenticated) dialer
		return &kafkago.Dialer{
			Timeout:   10 * time.Second,
			DualStack: false,
		}, nil
	}
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func TLSConfig(creds *Credentials) (*tls.Config, *Xk6KafkaError) {
	var clientCertFile = &creds.ClientCertPem
	if !FileExists(*clientCertFile) {
		return nil, NewXk6KafkaError(fileNotFound, "Client certificate file not found.", nil)
	}

	var clientKeyFile = &creds.ClientKeyPem
	if !FileExists(*clientKeyFile) {
		return nil, NewXk6KafkaError(fileNotFound, "Client key file not found.", nil)
	}

	var cert, err = tls.LoadX509KeyPair(*clientCertFile, *clientKeyFile)
	if err != nil {
		return nil, NewXk6KafkaError(
			failedLoadX509KeyPair,
			fmt.Sprintf("Error creating x509 keypair from client cert file \"%s\" and client key file \"%s\"", *clientCertFile, *clientKeyFile),
			err)
	}

	var caCertFile = &creds.ServerCaPem
	if !FileExists(*caCertFile) {
		return nil, NewXk6KafkaError(fileNotFound, "CA certificate file not found.", nil)
	}

	caCert, err := os.ReadFile(*caCertFile)
	if err != nil {
		// This might happen on permissions issues or if the file is unreadable somehow
		return nil, NewXk6KafkaError(
			failedReadCaCertFile,
			fmt.Sprintf("Error reading CA certificate file \"%s\"", *caCertFile),
			err)
	}
	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return nil, NewXk6KafkaError(
			failedAppendCaCertFile,
			fmt.Sprintf("Error appending CA certificate file \"%s\"", *caCertFile),
			nil)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
