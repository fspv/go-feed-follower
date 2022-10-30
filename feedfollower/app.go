package feedfollower

import (
	"context"
	"github.com/mmcdole/gofeed"
	// "github.com/mmcdole/gofeed/rss"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"
)

type FeedUpdate struct {
	feedId      uint
	feedTitle   string
	feedLink    string
	title       string
	description string
	url         string
}

func (feedUpdate *FeedUpdate) Hash() string {
	return (*feedUpdate).title + "|" + (*feedUpdate).url
}

type Observer interface {
	Id() uint
	Notify(feedProcessorUpdate FeedUpdate)
}

type Observable interface {
	Subscribe(observer Observer)
	Unsubscribe(observer Observer) bool
	NotifySubscribers(feedProcessorUpdate FeedUpdate)
}

type TelegramNotificationChannel struct {
	id                          uint
	userProcessor               *UserProcessor
	notificationType            string
	feedProcessorUpdatesChannel chan FeedUpdate // TODO: is it passed correctly by reference?
	active                      bool
	wg                          *sync.WaitGroup
	ctx                         context.Context
}

func (notificationChannel *TelegramNotificationChannel) Run() {
	defer notificationChannel.wg.Done()
	defer notificationChannel.Close()

	(*notificationChannel).active = true

	db := connectDatabase()
	var notification NotificationChannel
	// TODO handle errors
	db.First(&notification, NotificationChannel{Id: notificationChannel.id})

	api := &TelegramBotAPI{}
	api.Start(os.Getenv("TELEGRAM_BOT_API_KEY"), true)

	for {
		// TODO Init telegram related stuff and connect to telegram
		select {
		case <-notificationChannel.ctx.Done():
			log.Println("[TelegramNotificaionChannel::Run] Requested shutdown")
			return
		case feedProcessorUpdate := <-(*notificationChannel).feedProcessorUpdatesChannel:

			// TODO: verify transactions logic
			db := connectDatabase()

			history := History{
				UserId:                notificationChannel.userProcessor.id,
				FeedId:                feedProcessorUpdate.feedId,
				NotificationChannedId: notificationChannel.id,
				ItemHash:              feedProcessorUpdate.Hash(),
				State:                 "sending",
			}

			result := db.FirstOrCreate(&history, history)

			log.Println(result)

			if result.Error != nil {
				log.Println("[TelegramNotificaionChannel::Run] Query failed", history, result)
			} else if history.State == "sending" {
				db.Transaction(
					func(tx *gorm.DB) error {
						result := tx.Debug().Clauses(clause.Locking{Strength: "UPDATE"}).Find(&history)

						if result.Error != nil {
							log.Println("[TelegramNotificaionChannel::Run] Query failed", result)
							return result.Error
						}

						if history.State == "sending" {
							// TODO Send message to telegram
							log.Println("[TelegramNotificaionChannel::Run] Sending telegram update", feedProcessorUpdate, notificationChannel)

							message := NewTelegramMessage(
								feedProcessorUpdate.title,
								feedProcessorUpdate.description,
								feedProcessorUpdate.url,
								feedProcessorUpdate.feedTitle,
								feedProcessorUpdate.feedLink,
							)
							err := api.SendMessage(notification.Identifier, message)

							if err == nil {
								history.State = "sent"
								result := tx.Debug().Save(&history)

								if result.Error != nil {
									log.Println("[TelegramNotificaionChannel::Run] Query failed", result)
									return result.Error
								}
							}
						}

						return nil
					},
				)
			}
		}
	}
	// TODO: periodically check liveness and shutdown (run Close) if no longer exists
}

func (notificationChannel *TelegramNotificationChannel) Notify(feedProcessorUpdate FeedUpdate) {
	log.Println("[TelegramNotificationChannel::Notify] Got notification", feedProcessorUpdate)
	if (*notificationChannel).active {
		(*notificationChannel).feedProcessorUpdatesChannel <- feedProcessorUpdate
	}
}

func (notificationChannel *TelegramNotificationChannel) Id() uint {
	return (*notificationChannel).id
}

func (notificationChannel *TelegramNotificationChannel) Close() {
	log.Println("[TelegramNotificationChannel::Close] Shutting down")

	(*notificationChannel).userProcessor.Unsubscribe(notificationChannel)

	(*notificationChannel).active = false

	close(notificationChannel.feedProcessorUpdatesChannel)
}

type UserProcessor struct {
	id                          uint
	feedProcessor               *FeedProcessor
	feedProcessorUpdatesChannel chan FeedUpdate
	observers                   sync.Map
	active                      bool
	wg                          *sync.WaitGroup
	ctx                         context.Context
}

func (userProcessor *UserProcessor) Subscribe(observer Observer) bool {
	// Returns false if subscribed already
	_, loaded := (*userProcessor).observers.LoadOrStore(observer.Id(), &observer)
	if !loaded {
		log.Println("[UserProcessor::Subscribe] New observer subscribed", observer, userProcessor)
	} else {
		log.Println("[UserProcessor::Subscribe] Observer is already subscribed", observer, userProcessor)
	}
	return !loaded
}

func (userProcessor *UserProcessor) Unsubscribe(observer Observer) {
	log.Println("[UserProcessor::Unsubscribe] Unsubscribing", observer)
	(*userProcessor).observers.Delete(observer.Id())
}

func (userProcessor *UserProcessor) Notify(feedProcessorUpdate FeedUpdate) {
	log.Println("[UserProcessor::Notify] Got notification", feedProcessorUpdate)

	if (*userProcessor).active {
		(*userProcessor).feedProcessorUpdatesChannel <- feedProcessorUpdate
	} else {
		log.Println("[UserProcessor::Notify] Ignoring notification", feedProcessorUpdate)
	}
}

func (userProcessor *UserProcessor) NotifySubscribers(feedProcessorUpdate FeedUpdate) {
	log.Println("[UserProcessor::NotifySubscribers] Notifying with", feedProcessorUpdate)
	(*userProcessor).observers.Range(func(key, value interface{}) bool {
		(*(value.(*Observer))).Notify(feedProcessorUpdate)
		return true
	})
}

func (userProcessor *UserProcessor) Id() uint {
	return (*userProcessor).id
}

func (userProcessor *UserProcessor) Close() {
	log.Println("[UserProcessor::Close] Shutting down")

	// Unsubscribe myself from the parent observervable
	(*userProcessor).feedProcessor.Unsubscribe(userProcessor)

	// Unsubscribe all my observers
	(*userProcessor).observers.Range(func(key, value interface{}) bool {
		userProcessor.Unsubscribe((*(value.(*Observer))))
		return true
	})

	userProcessor.active = false

	close(userProcessor.feedProcessorUpdatesChannel)
}

func (userProcessor *UserProcessor) SyncNotificationChannels() {
	db := connectDatabase()
	rows, err := db.Model(&NotificationChannel{}).Where("user_id = ?", (*userProcessor).id).Rows()

	if err != nil {
		panic("Failed to fetch results from the database")
	}
	for rows.Next() {
		var notificationChannel NotificationChannel

		db.ScanRows(rows, &notificationChannel)

		log.Println("[UserProcessor::SyncNotificationChannels] Got a channel from db", notificationChannel)

		telegramNotificationChannel := TelegramNotificationChannel{
			id:                          notificationChannel.Id,
			userProcessor:               userProcessor,
			notificationType:            notificationChannel.Type,
			feedProcessorUpdatesChannel: make(chan FeedUpdate),
			wg:                          userProcessor.wg,
			ctx:                         userProcessor.ctx,
		}

		log.Println("[UserProcessor::SyncNotificationChannel] Syncing notification channel", telegramNotificationChannel, userProcessor)
		if userProcessor.Subscribe(&telegramNotificationChannel) {
			telegramNotificationChannel.wg.Add(1)
			go telegramNotificationChannel.Run()
		}
	}
}

func (userProcessor *UserProcessor) Run() {
	defer userProcessor.wg.Done()
	defer userProcessor.Close()

	(*userProcessor).active = true

	// Regularily update notification channel list
	log.Println("[UserProcessor::Run] Starting", userProcessor)

	wg := sync.WaitGroup{}

	// First run must be completed before running otherwise we risk losing the updates
	userProcessor.SyncNotificationChannels()

	wg.Add(1)
	go RunPeriodically(userProcessor.SyncNotificationChannels, 120*time.Second, &wg, userProcessor.ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-userProcessor.ctx.Done():
				log.Println("[UserProcessor::Run] Requested shutdown, exiting", userProcessor)
				return
			case feedProcessorUpdate := <-(*userProcessor).feedProcessorUpdatesChannel:
				// On new feedProcessor update fan out to all the notification channels
				(*userProcessor).NotifySubscribers(feedProcessorUpdate)
			}
		}
	}()

	wg.Wait()

	// TODO support unsubscription of deleted UserProcessors
}

type FeedProcessor struct {
	id               uint
	observers        sync.Map
	updatesGenerator FeedUpdateGenerator
	wg               *sync.WaitGroup
	ctx              context.Context
}

func (feedProcessor *FeedProcessor) Subscribe(observer Observer) bool {
	// Returns false if subscribed already
	_, loaded := (*feedProcessor).observers.LoadOrStore(observer.Id(), &observer)
	if !loaded {
		log.Println("[FeedProcessor::Subscribe] New observer subscribed", observer)
	} else {
		log.Println("[FeedProcessor::Subscribe] Observer is already subscribed", observer)
	}
	return !loaded
}

func (feedProcessor *FeedProcessor) Unsubscribe(observer Observer) {
	log.Println("[FeedProcessor::Unsubscribe] Unsubscribing", observer)
	(*feedProcessor).observers.Delete(observer.Id())
}

func (feedProcessor *FeedProcessor) NotifySubscribers(feedProcessorUpdate FeedUpdate) {
	log.Println("[FeedProcessor::NotifySubscribers] Notifying with", feedProcessorUpdate)

	(*feedProcessor).observers.Range(func(key, value interface{}) bool {
		(*(value.(*Observer))).Notify(feedProcessorUpdate)
		return true
	})
}

func (feedProcessor *FeedProcessor) FetchUpdates() {
	log.Println("[FeedProcessor::FetchUpdates] Starting", feedProcessor)

	feedUpdateGenerator := feedProcessor.updatesGenerator

	for feedProcessorUpdate := range feedUpdateGenerator.UpdatesChannel() {
		log.Println("[FeedProcessor::FetchUpdates] Got update", feedProcessorUpdate)
		(*feedProcessor).NotifySubscribers(feedProcessorUpdate)
	}
}

func (feedProcessor *FeedProcessor) SyncUserProcessors() {
	/*
		Best effort function to generate per user processors of feed updates.

		Reads from the database all the users subscribed to this feed and creates
		processors for each one of them.

		We need to run this periodically, because the user's subscriptions might change
	*/
	log.Println("[FeedProcessor::SyncUserProcessors] Fetching data", feedProcessor)
	// TODO add timeout of processors execution
	db := connectDatabase()
	rows, err := db.Model(&UserFeed{}).Where("feed_id = ?", (*feedProcessor).id).Rows()

	if err != nil {
		log.Println("Failed to fetch results from the database", err)
		return
	}
	for rows.Next() {
		var userFeed UserFeed

		db.ScanRows(rows, &userFeed)

		log.Println("[FeedProcessor::SyncUserProcessors] Got user feed entry", userFeed)

		userProcessor := UserProcessor{
			id:                          userFeed.UserId,
			feedProcessor:               feedProcessor,
			feedProcessorUpdatesChannel: make(chan FeedUpdate),
			observers:                   sync.Map{},
			ctx:                         feedProcessor.ctx,
			wg:                          feedProcessor.wg,
		}
		if feedProcessor.Subscribe(&userProcessor) {
			feedProcessor.wg.Add(1)
			go (&userProcessor).Run()
		}
	}
}

func (feedProcessor *FeedProcessor) Close() {
	log.Println("[FeedProcessor::Close] Shutting down")

	// Unsubscribe all my observers
	(*feedProcessor).observers.Range(func(key, value interface{}) bool {
		feedProcessor.Unsubscribe((*(value.(*Observer))))
		return true
	})
}

func (feedProcessor *FeedProcessor) Run() {
	defer feedProcessor.wg.Done()
	defer feedProcessor.Close()

	log.Println("[FeedProcessor::Run] Run", feedProcessor)

	feedProcessor.SyncUserProcessors() // It must be completed at least once, so we don't send updates into the void

	wgFeedProcessorTasks := sync.WaitGroup{}

	wgFeedProcessorTasks.Add(1)
	go RunPeriodically(feedProcessor.SyncUserProcessors, 120*time.Second, &wgFeedProcessorTasks, feedProcessor.ctx)
	wgFeedProcessorTasks.Add(1)
	go func() {
		defer wgFeedProcessorTasks.Done()
		feedProcessor.FetchUpdates()
	}()
	wgFeedProcessorTasks.Wait()
}

/*
Idea: have a fetch updates generator interface, so we can pass it to feed generator
Once passed we call function FetchUpdates, which populates Updates field, which is later consumed by
the feed processor

This way we can pass a mocked struct in the tests


TODO: make it an interface and actually pass it through the code
*/

type FeedUpdateGenerator interface {
	Run()
	UpdatesChannel() chan FeedUpdate
	Close()
}

// Implements FeedUpdateGenerator
type RssFeedUpdateGenerator struct {
	FeedId  uint
	Updates chan FeedUpdate
	wg      *sync.WaitGroup
	ctx     context.Context
}

func (rssFeedUpdateGenerator *RssFeedUpdateGenerator) Run() {
	defer rssFeedUpdateGenerator.Close()

	db := connectDatabase()

	var feed Feed

	db.First(&feed, &Feed{Id: rssFeedUpdateGenerator.FeedId})

	wg := sync.WaitGroup{}

	wg.Add(1)
	go RunPeriodically(
		func() {
			fp := gofeed.NewParser()
			feed, err := fp.ParseURL(feed.Url)

			if err != nil {
				log.Println(err)
				return
			}

			// TODO: rate limit instead of configuring number of last items
			for _, item := range feed.Items {
				feedUpdate := FeedUpdate{
					feedId:      rssFeedUpdateGenerator.FeedId,
					feedTitle:   feed.Title,
					feedLink:    feed.Link,
					title:       item.Title,
					description: item.Description,
					url:         item.Link,
				}
				rssFeedUpdateGenerator.Updates <- feedUpdate
			}
		},
		120*time.Second,
		&wg,
		(*rssFeedUpdateGenerator).ctx,
	)
	wg.Wait()
}

func (rssFeedUpdateGenerator *RssFeedUpdateGenerator) UpdatesChannel() chan FeedUpdate {
	return rssFeedUpdateGenerator.Updates
}

func (rssFeedUpdateGenerator *RssFeedUpdateGenerator) Close() {
	defer rssFeedUpdateGenerator.wg.Done()

	close(rssFeedUpdateGenerator.Updates)
}

type FeedProcessors struct {
	Processors *sync.Map
	wg         *sync.WaitGroup
	ctx        context.Context
}

func (feedProcessors *FeedProcessors) Update() {
	db := connectDatabase()
	rows, err := db.Model(&Feed{}).Rows()

	if err != nil {
		panic("Failed to fetch results from the database")
	}

	for rows.Next() {
		var feed Feed
		db.ScanRows(rows, &feed)

		_, loaded := (*(*feedProcessors).Processors).LoadOrStore(feed.Id, true)

		if !loaded {
			log.Println("Initializing feed processor", feed.Id)

			feedUpdateGenerator := &RssFeedUpdateGenerator{
				FeedId:  feed.Id,
				Updates: make(chan FeedUpdate),
				wg:      feedProcessors.wg,
				ctx:     (*feedProcessors).ctx,
			}
			feedProcessors.wg.Add(1)
			go feedUpdateGenerator.Run()

			feedProcessor := &FeedProcessor{
				id:               feed.Id,
				observers:        sync.Map{},
				updatesGenerator: feedUpdateGenerator,
				ctx:              (*feedProcessors).ctx,
				wg:               feedProcessors.wg,
			}
			feedProcessors.wg.Add(1)
			go feedProcessor.Run()
		}
	}
}

func Run() {
	log.Println("Initializing database")
	initDatabase()

	wg := sync.WaitGroup{}

	// Handle keyboard interrupts
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	defer func() {
		signal.Stop(signalChan)
		cancel()
	}()
	go func() {
		select {
		case <-signalChan: // first signal, cancel context
			log.Println("Received graceful shutdown request")
			cancel()
		case <-ctx.Done():
			log.Println("Shutting down now")
		}
		<-signalChan // second signal, hard exit
		log.Println("Requested a signal to shut down immediately")
		os.Exit(0)
	}()

	feedProcessors := FeedProcessors{
		Processors: &sync.Map{},
		wg:         &wg,
		ctx:        ctx,
	}

	wg.Add(1)
	go RunPeriodically(feedProcessors.Update, 120*time.Second, &wg, ctx)

	api := &TelegramBotAPI{}
	api.Start(os.Getenv("TELEGRAM_BOT_API_KEY"), true)
	wg.Add(1)
	go func() {
		defer wg.Done()
		api.HandleUpdates(ctx)
	}()

	wg.Wait()
}
