package feedfollower

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
	"gorm.io/driver/postgres"
)

// TODO: add foreign keys
type User struct {
	Id uint `gorm:"primaryKey"`
}

type NotificationChannel struct {
	Id         uint `gorm:"primaryKey"`
	UserId     uint
	Type       string `gorm:"index:idx_type_identifier,unique"`
	Identifier string `gorm:"index:idx_type_identifier,unique"`
	AuthData   string // TODO: JSON
}

type Feed struct {
	Id      uint   `gorm:"primaryKey"`
	Type    string `gorm:"index:idx_type_url,unique"` // TODO: Actually enum
	Url     string `gorm:"index:idx_type_url,unique"`
	Options string // TODO: JSON
}

type UserFeed struct {
	// TODO: first 2 primary key
	// TODO: needs index on user id and feed id
	UserId  uint   `gorm:"primaryKey"`
	FeedId  uint   `gorm:"primaryKey"`
	Options string // TODO: JSON
}

type History struct {
	// TODO: first 4 - primary key
	// TODO: needs index
	UserId                uint   `gorm:"primaryKey"`
	FeedId                uint   `gorm:"primaryKey"`
	NotificationChannedId uint   `gorm:"primaryKey"`
	ItemHash              string `gorm:"primaryKey"`
	State                 string // TODO: should be enum
}

func connectDatabase() *gorm.DB {
	return connectDatabaseEnableTransactions(false)
}

func connectDatabaseEnableTransactions(transactions bool) *gorm.DB {

	var (
		// Either a DB_USER or a DB_IAM_USER should be defined. If both are
		// defined, DB_IAM_USER takes precedence.
		dbUser                 = os.Getenv("DB_USER")                  // e.g. 'my-db-user'
		dbIAMUser              = os.Getenv("DB_IAM_USER")              // e.g. 'sa-name@project-id.iam'
		dbPwd                  = os.Getenv("DB_PASS")                  // e.g. 'my-db-password'
		dbName                 = os.Getenv("DB_NAME")                  // e.g. 'my-database'
		instanceConnectionName = os.Getenv("INSTANCE_CONNECTION_NAME") // e.g. 'project:region:instance'
		// usePrivate             = os.Getenv("PRIVATE_IP")
	)

	var driver gorm.Dialector

	if dbUser == "" {
		driver = sqlite.Open("test.db")
	} else {
		if dbUser == "" && dbIAMUser == "" {
			log.Fatal("Warning: One of DB_USER or DB_IAM_USER must be defined")
		}
		dsn := fmt.Sprintf("host=%s user=%s password=%s database=%s sslmode=disable", instanceConnectionName, dbUser, dbPwd, dbName)

		driver = postgres.New(postgres.Config{
			DriverName: "cloudsqlpostgres",
			DSN:        dsn,
		})
	}

	db, err := gorm.Open(
		driver,
		&gorm.Config{
			Logger:                 logger.Default.LogMode(logger.Info),
			SkipDefaultTransaction: transactions,
		},
	)
	if err != nil {
		panic("failed to connect database")
	}

	return db
}

func initDatabase() {
	db := connectDatabase()

	// Migrate the schema
	db.AutoMigrate(&User{})
	db.AutoMigrate(&NotificationChannel{})
	db.AutoMigrate(&Feed{})
	db.AutoMigrate(&UserFeed{})
	db.AutoMigrate(&History{})
}
