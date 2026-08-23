package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/relay"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
)

type clientConfig struct {
	Key      string `json:"key"`
	Endpoint string `json:"endpoint"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("bindery-udp-relay 0.1.0")
		return
	}
	config := relay.Config{DatagramLimit: envInt("BINDERY_RELAY_DATAGRAM_LIMIT", relayv1.DefaultDatagramLimit), PacketsPerSecond: envInt("BINDERY_RELAY_PACKETS_PER_SECOND", 1000), BytesPerSecond: envInt("BINDERY_RELAY_BYTES_PER_SECOND", 1<<20)}
	service := relay.New(config)
	if err := service.Start(); err != nil {
		logger.Error("relay start failed", "error", err)
		os.Exit(1)
	}
	allocationID := os.Getenv("BINDERY_RELAY_ALLOCATION_ID")
	clients, endpoints, err := loadClients(os.Getenv("BINDERY_RELAY_CLIENTS"))
	if err != nil {
		logger.Error("relay clients invalid", "error", err)
		os.Exit(1)
	}
	if allocationID != "" {
		leaseSeconds := envInt("BINDERY_RELAY_LEASE_SECONDS", 86400)
		if err := service.RegisterAllocation(allocationID, clients, time.Now().UTC().Add(time.Duration(leaseSeconds)*time.Second)); err != nil {
			logger.Error("allocation registration failed", "error", err)
			os.Exit(1)
		}
	}
	listenAddr := os.Getenv("BINDERY_RELAY_ADDR")
	if listenAddr == "" {
		listenAddr = ":40000"
	}
	connection, err := net.ListenUDP("udp", mustUDPAddr(listenAddr))
	if err != nil {
		logger.Error("udp listen failed", "error", err)
		os.Exit(1)
	}
	defer connection.Close()
	go serveAdmin(service, os.Getenv("BINDERY_RELAY_ADMIN_ADDR"))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		_ = service.BeginDrain()
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	}()
	buffer := make([]byte, 64*1024)
	for {
		length, source, readErr := connection.ReadFromUDP(buffer)
		if readErr != nil {
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				break
			}
			if errors.Is(readErr, net.ErrClosed) {
				break
			}
			logger.Error("udp read failed", "error", readErr)
			break
		}
		datagram := append([]byte(nil), buffer[:length]...)
		header, peekErr := relayv1.Peek(datagram, config.DatagramLimit)
		if peekErr != nil {
			continue
		}
		if endpoint, ok := endpoints[header.SenderID]; ok && endpoint.String() != source.String() {
			continue
		}
		_ = service.Forward(datagram, header.SenderID, time.Now().UTC(), func(recipientID string, output []byte) error {
			endpoint, ok := endpoints[recipientID]
			if !ok {
				return relay.ErrRecipientNotFound
			}
			_, err := connection.WriteToUDP(output, endpoint)
			return err
		})
	}
}

func serveAdmin(service *relay.Relay, address string) {
	if address == "" {
		address = ":9090"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		state := service.State()
		if state != relay.Accepting && state != relay.Draining {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/drain", func(w http.ResponseWriter, _ *http.Request) {
		_ = service.BeginDrain()
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		metrics := service.Metrics()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(w, "bindery_relay_packets_forwarded %d\nbindery_relay_bytes_forwarded %d\nbindery_relay_packets_dropped %d\nbindery_relay_accepting_capacity %d\n", metrics.PacketsForwarded, metrics.BytesForwarded, metrics.PacketsDropped, boolMetric(service.State() == relay.Accepting))
	})
	_ = http.ListenAndServe(address, mux)
}

func loadClients(raw string) (map[string][]byte, map[string]*net.UDPAddr, error) {
	result := make(map[string][]byte)
	endpoints := make(map[string]*net.UDPAddr)
	if raw == "" {
		return result, endpoints, nil
	}
	var values map[string]clientConfig
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, nil, err
	}
	for id, value := range values {
		key, err := base64.RawURLEncoding.DecodeString(value.Key)
		if err != nil || len(key) != relayv1.TransportKeyBytes {
			return nil, nil, fmt.Errorf("client %s key must be raw-base64url 32 bytes", id)
		}
		endpoint, err := net.ResolveUDPAddr("udp", value.Endpoint)
		if err != nil {
			return nil, nil, err
		}
		result[id] = key
		endpoints[id] = endpoint
	}
	return result, endpoints, nil
}

func mustUDPAddr(value string) *net.UDPAddr {
	endpoint, err := net.ResolveUDPAddr("udp", value)
	if err != nil {
		panic(err)
	}
	return endpoint
}
func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
}
