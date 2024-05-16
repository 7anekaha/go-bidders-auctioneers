build:
	go build -o auc ./auctioneer/main.go
	go build -o bid ./bidder/main.go

run-auc: 
	./auc

run-bid:
	./bid --name $(name)
