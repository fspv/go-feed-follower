package main

import (
	"fmt"
	"github.com/prius/go-feed-follower/feedfollower"
	"net/http"
)

func main() {
	http.HandleFunc("/", handler)
	go http.ListenAndServe(":8080", nil)
	feedfollower.Run()
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "ok")
}
