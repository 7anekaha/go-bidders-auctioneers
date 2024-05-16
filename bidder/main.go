package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/7anekaha/go-bidder-auctioner/proto"
	"google.golang.org/grpc"
)
var userID string
var bidderName string

func ListenAllAds(done chan bool, stream pb.AdService_ListenRequestsClient) chan *pb.AdRequest{
	adsCh := make(chan *pb.AdRequest)
	

	go func() {
		defer close(adsCh)

		for {
			select {
			case <-done:
				return
			default:
				ad, err := stream.Recv()
				if err != nil {
					log.Fatalf("[%v] Error receiving ad: %v\n",bidderName, err)
				}
				log.Printf("[%v] Received Ad: %v, started at:%v, duration: %v\n",bidderName, ad.GetAdID(), ad.GetStartTimestamp(), ad.GetDuration())
				adsCh <- ad
			}
		}
	}()
	return adsCh
}

func MakeBids(done chan bool, client pb.AdServiceClient, adsCh chan *pb.AdRequest) {
	for ad := range adsCh {
		// check channel closed
		if ad == nil {
			return
		}
		go func (done chan bool, ad *pb.AdRequest) {
			// run this randomly
			numTimes := rand.Intn(1000)
			for i:= 0; i< numTimes; i++ {
				waitTime := rand.Intn(2000) // max 2 seconds
				percentage := int64(rand.Intn(100))
				amount := ad.GetAmount() +( (ad.GetAmount() * percentage)/100 )
				log.Printf("[%v] Waiting for %v ms\n", bidderName, waitTime)

				select {
				case <-done:
					return
				case <- time.After(time.Duration(waitTime) * time.Millisecond):
					bid := &pb.AdResponse{
						AdID: ad.GetAdID(),
						UserID: userID,
						Amount: amount,
					}
					log.Printf("[%v] Bidding %v for Ad %s\n",bidderName, amount, ad.GetAdID())
					resp, err := client.Bid(context.Background(), bid)
					log.Printf("[%v] Bid response for AD %s: %v\n",bidderName, ad.GetAdID(), resp)

					if err != nil {
						log.Printf("[%v] Error bidding: %v\n",bidderName, err)
						return
					}
					if resp.GetStatus() == pb.Status_CLOSED || resp.GetError() == pb.Error_AD_CLOSED{
						log.Printf("[%v] Ad %s closed\n",bidderName, ad.GetAdID())
						return
					}

					isMyBidWinning := resp.GetUserID() == userID
					log.Printf("[%v] My bid is winning: %v\n",bidderName, isMyBidWinning)					
				}				
			}
		}(done, ad)
	}
}

func main() {

	var name string
	flag.StringVar(&name, "name", "Foo", "Bidder name")
	flag.Parse()
	bidderName = "Bidder-" + name

	done := make(chan bool)

	conn, err := grpc.Dial("localhost:4000", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("[%v] could not connect: %v\n", bidderName, err)
	}
	defer conn.Close()

	client := pb.NewAdServiceClient(conn)

	log.Printf("[%v] Connected to server\n", bidderName)

	// get user ID
	userResp, err := client.Connect(context.Background(), &pb.UserRequest{Name: bidderName})
	if err != nil {
		log.Fatalf("[%v] could not connect: %v\n", bidderName, err)
	}
	log.Printf("[%v] Connected with userID: %v\n", bidderName, userResp.GetUserID())
	userID = userResp.GetUserID()

	// listen for ads
	adsStream, err := client.ListenRequests(context.Background(), &pb.UserResponse{UserID: userID})
	if err != nil {
		log.Fatalf("[%v] could not listen for ads: %v\n", bidderName, err)
	}
	adsCh := ListenAllAds(done, adsStream)
	
	go MakeBids(done, client, adsCh)

	// Wait for Ctrl+C to exit
	go func(){
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		close(done)
	}()
	
	<- done
}
