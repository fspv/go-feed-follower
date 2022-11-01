package feedfollower

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Telegram Bot api instance should be created only once to avoid duplicate messagees
var TelegramBotApiSingleton *tgbotapi.BotAPI
var TelegramBotApiBotOnce = &sync.Once{}

var TelegramSendMessageLock = &sync.Mutex{}

func NewTelegramNotificationChannel(identifier string) {
	// TODO: handle errors

	db := connectDatabase()
	var notificationChannel NotificationChannel
	db.FirstOrCreate(&notificationChannel, NotificationChannel{Type: "telegram", Identifier: identifier})

	if notificationChannel.UserId == 0 {
		log.Println("[NewTelegramNotificationChannel] User not found, creating a new one")
		var user User
		db.Create(&user)
		notificationChannel.UserId = user.Id

		log.Println("[NewTelegramNotificationChannel] Created a user with id: ", user.Id)

		db.Save(&notificationChannel)
	} else {
		log.Println("[NewTelegramNotificationChannel] User and notification channel already exists", notificationChannel)
	}
}

func NewRssSubscription(url string, identifier string) {
	// TODO: handle errors

	log.Println("[NewRssSubscription] Creating a new subscription", url, identifier)
	db := connectDatabase()

	var feed Feed
	db.FirstOrCreate(&feed, &Feed{Type: "rss", Url: url})

	var notificationChannel NotificationChannel
	db.First(&notificationChannel, NotificationChannel{Type: "telegram", Identifier: identifier})

	var userFeed UserFeed

	db.FirstOrCreate(&userFeed, &UserFeed{UserId: notificationChannel.UserId, FeedId: feed.Id})
}

func DeleteRssSubscription(url string, identifier string) {

	log.Println("[DeleteRssSubscription] Deleting a subscription", url, identifier)
	db := connectDatabase()

	var feed Feed
	db.First(&feed, &Feed{Type: "rss", Url: url})

	var notificationChannel NotificationChannel
	db.First(&notificationChannel, NotificationChannel{Type: "telegram", Identifier: identifier})

	db.Delete(&UserFeed{UserId: notificationChannel.UserId, FeedId: feed.Id})
}

func GetSubscriptions(identifier string) []string {
	// TODO handle errors
	subscriptions := []string{}

	log.Println("[GetSubscriptions] Fetching subscriptions for ", identifier)
	db := connectDatabase()

	var notificationChannel NotificationChannel
	db.First(&notificationChannel, NotificationChannel{Type: "telegram", Identifier: identifier})

	// userFeeds := []UserFeed{}

	rows, err := db.Model(&UserFeed{UserId: notificationChannel.UserId}).Rows()

	if err != nil {
		panic("Failed to fetch results from the database")
	}
	for rows.Next() {
		var userFeed UserFeed
		db.ScanRows(rows, &userFeed)

		var feed Feed
		db.First(&feed, &Feed{Id: userFeed.FeedId})

		subscriptions = append(subscriptions, feed.Url)
	}

	return subscriptions
}

type TelegramBotAPI struct {
	Bot *tgbotapi.BotAPI
}

func (api *TelegramBotAPI) Start(token string, debug bool) error {
	log.Println("[TelegramBotAPI::Start] Initialising bot api")

	// Telegram Bot api instance should be created only once to avoid duplicate messagees
	TelegramBotApiBotOnce.Do(
		func() {
			for {
				var err error

				tmp, err := tgbotapi.NewBotAPI(token)

				if err != nil {
					log.Println("[TelegramBotApi::Start] Failed to start, retrying: ", err)
				} else {
					TelegramBotApiSingleton = tmp

					break
				}
			}
		},
	)

	bot := TelegramBotApiSingleton

	log.Printf("[TelegramBotApi::Start] Authorized on account %s", bot.Self.UserName)

	bot.Debug = debug

	api.Bot = bot

	return nil
}

func (api *TelegramBotAPI) Stop() {
	api.Bot.StopReceivingUpdates()
}

func (api *TelegramBotAPI) HandleUpdates(ctx context.Context) {
	// TODO: the only way to shut this down is to kill the app
	// Contexts are currently not supported by the tg bot api lib
	// so for now no straightforward way to fix this
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	commandConfig := tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{
			Command:     "/subscribe",
			Description: "Subscribe to an arbitrary feed (provide URL as an argument)",
		},
		tgbotapi.BotCommand{
			Command:     "/unsubscribe",
			Description: "Unsubscribe from a feed",
		},
		tgbotapi.BotCommand{
			Command:     "/list",
			Description: "List all the feeds you're subscribed to",
		},
	)

	log.Println("[TelegramBotApi::Start] Commands config", commandConfig)

	for {

		TelegramSendMessageLock.Lock()
		response, err := func() (*tgbotapi.APIResponse, error) {
			defer TelegramSendMessageLock.Unlock()
			return api.Bot.Request(commandConfig)
		}()

		if err != nil {
			log.Println("[TelegramBotApi::Start] Error while processing request", response, err)
			if response.ErrorCode == 429 {
				// Request rate limited, try again later
				log.Println("[TelegramBotApi::Start] Retrying after ", response.Parameters.RetryAfter)
				time.Sleep(time.Duration(response.Parameters.RetryAfter) * time.Second)
			} else {
				panic(err)
			}
		} else {
			log.Println("[TelegramBotApi::Start] Request ok", response)
			break
		}
	}

	updates := api.Bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			log.Println("[TelegramBotApi::HandleUpdates] Stop listening for telegram updates")
			return
		case update := <-updates:

			if update.Message == nil { // ignore any non-Message updates
				log.Println("This is not a message", update)
				continue
			}
			if !update.Message.IsCommand() { // ignore any non-command Messages
				log.Println("This is not a command", update)
				continue
			}

			commandStartOffset := update.Message.Entities[0].Offset
			commandEndOffset := commandStartOffset + update.Message.Entities[0].Length

			var argsString string

			if commandEndOffset+1 < len(update.Message.Text) {
				argsString = update.Message.Text[commandEndOffset+1:]
			} else {
				argsString = ""
			}

			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")

			switch update.Message.Command() {
			case "start":
				msg.Text = "Welcome! Use /subscribe command with URL as an argument to subscribe to RSS"
				NewTelegramNotificationChannel(strconv.Itoa(int(update.Message.Chat.ID)))
			case "subscribe":
				// TODO: limit number of subscriptions
				if argsString == "" {
					msg.Text = "Please provide a url after the command"
				} else {
					url, err := url.ParseRequestURI(strings.TrimSpace(argsString))
					if err == nil {
						fp := gofeed.NewParser()
						_, err := fp.ParseURL(url.String())

						if err != nil {
							msg.Text = "Incorrect feed, failed to parse" + fmt.Sprint(err)
							log.Println("Failed to parse feed", err)
						} else {
							msg.Text = "Subscribed to " + url.String()
							NewRssSubscription(url.String(), strconv.Itoa(int(update.Message.Chat.ID)))
						}
					} else {
						msg.Text = "Error parsing url: " + fmt.Sprint(err)
					}
				}
			case "unsubscribe":
				if argsString == "" {
					// TOOD: paginate list
					subscriptions := GetSubscriptions(strconv.Itoa(int(update.Message.Chat.ID)))

					buttons := [][]tgbotapi.KeyboardButton{}

					for _, subscription := range subscriptions {
						buttons = append(buttons, tgbotapi.NewKeyboardButtonRow(tgbotapi.NewKeyboardButton("/unsubscribe "+subscription)))
					}

					numericKeyboard := tgbotapi.NewReplyKeyboard(buttons...)

					msg.Text = "Select a feed to unsubscribe from"
					msg.ReplyMarkup = numericKeyboard
				} else {
					msg.Text = "Unsubscribed from " + argsString
					msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)

					DeleteRssSubscription(argsString, strconv.Itoa(int(update.Message.Chat.ID)))
				}
			case "list":
				// TOOD: paginate list
				subscriptions := GetSubscriptions(strconv.Itoa(int(update.Message.Chat.ID)))

				subscriptionList := ""

				for _, subscription := range subscriptions {
					subscriptionList = subscriptionList + subscription + "\n"
				}

				msg.Text = "List of all the subscriptions:\n" + subscriptionList
			default:
				msg.Text = "I don't know that command: " + update.Message.Command()
			}
			msg.ReplyToMessageID = update.Message.MessageID

			TelegramSendMessageLock.Lock()
			func() {
				defer TelegramSendMessageLock.Unlock()
				api.Bot.Send(msg)
			}()
		}
	}
}

func NewTelegramMessage(
	title string,
	description string,
	url string,
	feedTitle string,
	feedLink string,
) string {
	p := bluemonday.NewPolicy()
	p.AllowStandardURLs()

	// We only allow <p> and <a href="">
	p.AllowAttrs("href").OnElements("a")
	p.AllowElements("b")

	message := fmt.Sprintf(
		"<b><a href='%s'>%s</a></b>\n%s\n\nFeed: <a href='%s'>%s</a>",
		url,
		title,
		description,
		feedLink,
		feedTitle,
	)

	return p.Sanitize(message)
}

func (api *TelegramBotAPI) SendMessage(idString string, message string) error {
	id, err := strconv.Atoi(idString)

	if err != nil {
		log.Println("[TelegramBotApi::SendMessaeg] Failed to convert handle to int", idString)
	}

	msg := tgbotapi.NewMessage(int64(id), message)

	msg.ParseMode = "HTML"

	TelegramSendMessageLock.Lock()
	_, err = func() (tgbotapi.Message, error) {
		defer TelegramSendMessageLock.Unlock()
		return api.Bot.Send(msg)
	}()

	if err != nil {
		log.Printf("Error %s sending message %s", err, msg.Text)
		return errors.New("Failed to send message")
	}

	return nil
}
