package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/cors"
)

var userIDIdx int = 3 // 3 because we have 3 users in the beginning

type AdRequest struct {
	Duration int64 `json:"duration"`
	MinBid   int64 `json:"min_bid"`
}

type AdResponse struct {
	ID          string    `json:"id"`
	Duration    int64     `json:"duration"`
	MinBid      int64     `json:"min_bid"`
	CreatedAt   time.Time `json:"created_at"`
	CurrentBid  int64     `json:"current_bid"`
	CurrentUser string    `json:"current_user"`
	NumBids     int       `json:"num_bids"`
}

type AllAdsResponse struct {
	Ads []AdResponse `json:"ads"`
}

type WinnersResponse struct {
	Winners []AdResponse `json:"winners"`
}

type User struct {
	UserID  string   `json:"user_id"`
	BidsWon []string `json:"bids_won"`
}

type UsersResponse struct {
	Users []User `json:"users"`
}

func NewAuction(duration, minBid int64) *AdResponse {
	userIDIdx++
	return &AdResponse{
		ID:          fmt.Sprintf("ad-%d", userIDIdx),
		Duration:    duration,
		MinBid:      minBid,
		CreatedAt:   time.Now(),
		CurrentBid:  minBid,
		CurrentUser: "",
		NumBids:     0,
	}
}

type RestServer struct {
	Mux           *http.ServeMux
	AuctionServer *AuctionServer
}

func (rs *RestServer) NewAdHandler(w http.ResponseWriter, r *http.Request) {
	body := r.Body
	defer body.Close()

	var auctionRequest AdRequest
	if err := json.NewDecoder(body).Decode(&auctionRequest); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	auction := NewAuction(auctionRequest.Duration, auctionRequest.MinBid)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(auction); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	jsonAuction, err := json.Marshal(auction)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	rs.AuctionServer.muAds.Lock()
	rs.AuctionServer.Ads[auction.ID] = &Ad{
		ID:          auction.ID,
		Duration:    auction.Duration,
		CreatedAt:   auction.CreatedAt,
		CurrentBid:  auction.CurrentBid,
		CurrentUser: auction.CurrentUser,
		MinBid:      float64(auction.MinBid),
		Status:      status(CREATED),
		NumBids:     auction.NumBids,
	}
	rs.AuctionServer.muAds.Unlock()

	w.Write(jsonAuction)
}

func (rs *RestServer) GetAdHandler(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	st := queryParams.Get("status")
	if st == "" {
		st = "3" // default status "all
	}

	statusFilter, err := strconv.Atoi(st)
	if err != nil {
		http.Error(w, "Invalid status", http.StatusBadRequest)
		return
	}

	filteredAds := make([]*Ad, 0)
	if statusFilter != 2 {
		rs.AuctionServer.muAds.RLock()
		log.Println("Ads: ", rs.AuctionServer.Ads)
		for _, ad := range rs.AuctionServer.Ads {
			log.Println("Ad status: ", ad.Status, statusFilter)
			switch statusFilter {
			case 0:
				if ad.Status == status(CREATED) {
					filteredAds = append(filteredAds, ad)
				}
			case 1:
				if ad.Status == status(OPEN) {
					filteredAds = append(filteredAds, ad)
				}
			case 3:
				filteredAds = append(filteredAds, ad)
			}
		}
		rs.AuctionServer.muAds.RUnlock()
	} else {
		rs.AuctionServer.muAdsClosed.RLock()
		for _, ad := range rs.AuctionServer.AdsClosed {
			filteredAds = append(filteredAds, ad)
		}
		rs.AuctionServer.muAdsClosed.RUnlock()

	}

	allAds := AllAdsResponse{
		Ads: make([]AdResponse, 0),
	}
	for _, ad := range filteredAds {
		allAds.Ads = append(allAds.Ads, AdResponse{
			ID:          ad.ID,
			Duration:    ad.Duration,
			MinBid:      int64(ad.MinBid),
			CreatedAt:   ad.CreatedAt,
			CurrentBid:  ad.CurrentBid,
			CurrentUser: ad.CurrentUser,
			NumBids:     ad.NumBids,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	jsonResponse, err := json.Marshal(allAds)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Write(jsonResponse)

}

func (rs *RestServer) GetWinnersHandler(w http.ResponseWriter, r *http.Request) {
	rs.AuctionServer.muAdsClosed.RLock()
	defer rs.AuctionServer.muAdsClosed.RUnlock()

	winners := WinnersResponse{
		Winners: make([]AdResponse, 0),
	}
	for _, ad := range rs.AuctionServer.AdsClosed {
		winners.Winners = append(winners.Winners, AdResponse{
			ID:          ad.ID,
			Duration:    ad.Duration,
			MinBid:      int64(ad.MinBid),
			CreatedAt:   ad.CreatedAt,
			CurrentBid:  ad.CurrentBid,
			CurrentUser: ad.CurrentUser,
			NumBids:     ad.NumBids,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	jsonResponse, err := json.Marshal(winners)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Write(jsonResponse)
}

func (rs *RestServer) GetUsersHandler(w http.ResponseWriter, r *http.Request) {

	users := make([]User, 0)
	mapUserToAd := make(map[string][]string)
	rs.AuctionServer.muClients.Lock()

	for _, client := range rs.AuctionServer.Clients {
		mapUserToAd[client.UserID] = []string{}
	}
	rs.AuctionServer.muClients.Unlock()

	rs.AuctionServer.muAdsClosed.RLock()
	for _, ad := range rs.AuctionServer.AdsClosed {
		mapUserToAd[ad.CurrentUser] = append(mapUserToAd[ad.CurrentUser], ad.ID)
	}
	rs.AuctionServer.muAdsClosed.RUnlock()

	for user, ads := range mapUserToAd {
		users = append(users, User{
			UserID:  user,
			BidsWon: ads,
		})
	}

	usersResponse := UsersResponse{
		Users: users,
	}

	w.Header().Set("Content-Type", "application/json")
	jsonResponse, err := json.Marshal(usersResponse)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Write(jsonResponse)
}

func (rs *RestServer) Run() error {
	rs.Mux.HandleFunc("POST /ads/new", rs.NewAdHandler)
	rs.Mux.HandleFunc("GET /ads/", rs.GetAdHandler)
	rs.Mux.HandleFunc("GET /ads/winners", rs.GetWinnersHandler)
	rs.Mux.HandleFunc("GET /users", rs.GetUsersHandler)
	handler := cors.Default().Handler(rs.Mux)
	return http.ListenAndServe(":8080", handler)
}
