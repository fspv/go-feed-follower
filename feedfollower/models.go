package feedfollower

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"sync"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/postgres"
	"gorm.io/driver/postgres"
)

var dbSingleton *gorm.DB
var dbSingletonLock = &sync.Mutex{}

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
	if dbSingleton == nil {
		dbSingletonLock.Lock()
		defer dbSingletonLock.Unlock()

		if dbSingleton == nil {
			dbSingleton = connectDatabaseEnableTransactions(false)
		}
	}

	return dbSingleton
}

func connectDatabaseEnableTransactions(transactions bool) *gorm.DB {
	config := getConfiguration()

	var (
		dbUser                 = config.PostgresUser
		dbIAMUser              = config.PostgresIamUser
		dbPwd                  = config.PostgresPassword
		dbName                 = config.PostgresDatabase
		instanceConnectionName = config.PostgresInstanceConnectionName
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

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to connect database")
	}

	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)

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
