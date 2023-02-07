package feedfollower

import (
	"context"
	"fmt"
	"log"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

type Configuration struct {
	PostgresUser                   string
	PostgresIamUser                string
	PostgresInstanceConnectionName string
	PostgresPassword               string
	PostgresDatabase               string
	PostgresConnectionName         string
	TelegramBotApiKey              string
}

func tryGetEnvVar(name string) (string, error) {
	result, found := os.LookupEnv(name)

	if !found {
		return "", fmt.Errorf("Can't find the secret %s", name)
	}

	return result, nil
}

func tryGetGoogleCloudSecret(name string) (string, error) {
	secretEnvVar := secretNameEnvVar(name)

	secretName, err := tryGetEnvVar(secretEnvVar)

	if err != nil {
		log.Fatalf("Can't get secret %s (%s)", secretName, secretEnvVar)
		return "", err
	}

	// Create the client.
	ctx := context.Background()
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		log.Fatalf("failed to setup client: %v", err)
	}
	defer client.Close()

	// Build the request.
	accessRequest := &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretName,
	}

	// Call the API.
	result, err := client.AccessSecretVersion(ctx, accessRequest)
	if err != nil {
		log.Fatalf("failed to access secret version: %v (%s)", err, secretName)
	}

	value := result.Payload.Data

	return string(value), nil
}

func secretNameEnvVar(name string) string {
	return "SECRET_" + name
}

func getConfValue(name string) string {
	result, err := tryGetEnvVar(name)

	if err == nil {
		return result
	}

	result, err = tryGetGoogleCloudSecret(name)

	if err != nil {
		log.Fatalf("Can't get a config value for %s", name)
	}

	return result
}

func getConfiguration() Configuration {
	// Either a DB_USER or a DB_IAM_USER should be defined. If both are
	// defined, DB_IAM_USER takes precedence.

	config := Configuration{
		PostgresUser:                   getConfValue("DB_USER"),                  // e.g. 'my-db-user'
		PostgresIamUser:                getConfValue("DB_IAM_USER"),              // e.g. 'sa-name@project-id.iam'
		PostgresPassword:               getConfValue("DB_PASS"),                  // e.g. 'my-db-password'
		PostgresDatabase:               getConfValue("DB_NAME"),                  // e.g. 'my-database'
		PostgresInstanceConnectionName: getConfValue("INSTANCE_CONNECTION_NAME"), // e.g. 'project:region:instance'
		TelegramBotApiKey:              getConfValue("TELEGRAM_BOT_API_KEY"),     // e.g. 'project:region:instance'
	}

	if config.PostgresUser == "" && config.PostgresIamUser == "" {
		log.Fatal("Warning: One of DB_USER or DB_IAM_USER must be defined")
	}

	return config
}
