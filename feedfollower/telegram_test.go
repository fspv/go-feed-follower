package feedfollower

import (
	"os"
	"testing"
)

func TestTelegramBot(t *testing.T) {
	initDatabase()
	db := connectDatabase()

	// TODO implement more meaningful tests here
	api := &TelegramBotAPI{}
	api.Start(os.Getenv("TELEGRAM_BOT_API_KEY"), true)
	api.HandleUpdates()

	// Cleanup
	db.Delete(&Feed{})
	db.Delete(&User{})
	db.Delete(&UserFeed{})
	db.Delete(&NotificationChannel{})
}
