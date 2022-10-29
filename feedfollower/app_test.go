package feedfollower

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"
)

func TestRunApp(t *testing.T) {
	initDatabase()

	db := connectDatabase()
	db.Create(&Feed{Id: 1, Type: "rss", Url: "https://test.com", Options: "{}"})
	db.Create(&Feed{Id: 2, Type: "rss", Url: "https://test2.com", Options: "{}"})
	db.Create(&User{Id: 1})
	db.Create(&UserFeed{UserId: 1, FeedId: 1, Options: "{}"})
	db.Create(
		&NotificationChannel{
			Id:         1,
			UserId:     1,
			Type:       "telegram",
			Identifier: "1477686658",
			AuthData:   "",
		},
	)

	wg := sync.WaitGroup{}
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	feed := &Feed{Id: 1, Type: "rss", Url: "", Options: "{}"}
	log.Println("Initializing feed processor", feed.Id)
	feedUpdateGenerator := &RssFeedUpdateGenerator{
		FeedId:  feed.Id,
		Updates: make(chan FeedUpdate),
		wg:      &wg,
		ctx:     ctx,
	}
	wg.Add(1)
	go feedUpdateGenerator.Run()

	feedProcessor := &FeedProcessor{id: feed.Id, observers: sync.Map{}, updatesGenerator: feedUpdateGenerator, wg: &wg, ctx: ctx}

	wg.Add(1)
	go feedProcessor.Run()

	time.Sleep(15 * time.Second)

	log.Println("[Test] Finishing context")
	cancel()
	log.Println("[Test] Waiting on wait group")
	wg.Wait()

	// Cleanup
	db.Delete(&Feed{})
	db.Delete(&User{})
	db.Delete(&UserFeed{})
	db.Delete(&NotificationChannel{})
}
