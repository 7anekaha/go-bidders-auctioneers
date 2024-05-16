package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/7anekaha/go-bidder-auctioner/proto"
	s "github.com/7anekaha/go-bidder-auctioner/auctioneer/services"
	"google.golang.org/grpc"
)




func main() {

	listener, err := net.Listen("tcp", ":4000")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	done := make(chan bool)
	server := &s.AuctionServer{
		Done:      done,
		Clients:   make(map[string]*s.Client),
		Ads:       make(map[string]*s.Ad),
		AdsClosed: make(map[string]*s.Ad),
	}

	restServer := &s.RestServer{
		Mux:           http.NewServeMux(),
		AuctionServer: server,
	}

	// start rest server
	go func() {
		log.Println("Starting rest server on port :8080")
		if err := restServer.Run(); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}

	}()

	grpcServer := grpc.NewServer()
	pb.RegisterAdServiceServer(grpcServer, server)

	// graceful shutdown
	go func(done chan bool) {
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
		<-signalChan
		log.Println("Shutting down server...")
		close(done)
	}(done)

	// start grpc server
	go func() {
		log.Println("Starting grpc server on port :4000")
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// fill auctions with some data - this should be done from POST /auctions/new endpoint
	s.StartNewAuctions(server)

	// log winner
	go s.CleanClosedAds(done, server)

	<-done
}
