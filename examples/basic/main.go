package main

import (
	"context"
	"log"
	"os"
	"time"

	infra "github.com/ivanhxy/youwei-infra-sdk"
)

func main() {
	client, err := infra.NewClient(infra.Config{
		BaseURL:      os.Getenv("INFRA_BASE_URL"),
		ClientID:     os.Getenv("INFRA_CLIENT_ID"),
		ClientSecret: os.Getenv("INFRA_CLIENT_SECRET"),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := client.Images.List(ctx, infra.ListImagesRequest{Page: 1, PageSize: 20})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("图片总数=%d 当前页=%d request_id=%s", result.Data.Total, len(result.Data.Images), result.RequestID)
}
