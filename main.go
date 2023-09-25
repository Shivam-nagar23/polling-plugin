package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shivam-nagar23/polling-plugin/bean"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"time"
)

const (
	AccessKey       = "ACCESS_KEY"
	SecretKey       = "SECRET_KEY"
	EndpointUrl     = "ENDPOINT_URL"
	AwsRegion       = "AWS_REGION"
	LastFetchedTime = "LAST_FETCHED_TIME"
	REPOSITORY      = "REPOSITORY"
	CiPipelineId    = "CI_PIPELINE_ID"
)

func setEnvForTesting() {
	os.Setenv(AccessKey, "AKIAWPTCEWL5XI5G3Q4Q")
	os.Setenv(SecretKey, "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")
	os.Setenv(EndpointUrl, "445808685819.dkr.ecr.us-east-2.amazonaws.com")
	os.Setenv(AwsRegion, "us-east-2")
	os.Setenv(LastFetchedTime, "")
	os.Setenv(REPOSITORY, "kushagratest")
	os.Setenv(CiPipelineId, "1")
}

func main() {
	fmt.Println("hello")
	//allRepos := strings.Split(REPOS, ",")
	setEnvForTesting()
	accessKey := os.Getenv(AccessKey)
	secretKey := os.Getenv(SecretKey)
	endpointUrl := os.Getenv(EndpointUrl)
	region := os.Getenv(AwsRegion)
	lastFetchedTime, err := parseTime(os.Getenv(LastFetchedTime))
	if err != nil {
		fmt.Println("error in parsing last fetched time, using time zero golang", err)
	}
	repoName := os.Getenv(REPOSITORY)
	ciPipelineId, err := strconv.Atoi(os.Getenv(CiPipelineId))
	if err != nil {
		fmt.Println("error in getting ciPipeline", err.Error())
		return
	}

	GetResultsAndSaveInFile(accessKey, secretKey, endpointUrl, region, repoName, lastFetchedTime, ciPipelineId)
}

func parseTime(timeString string) (time.Time, error) {
	layout := "2006-01-02T15:04:05.000Z"
	t, err := time.Parse(layout, timeString)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

type AwsBaseConfig struct {
	AccessKey   string `json:"accessKey"`
	Passkey     string `json:"passkey"`
	EndpointUrl string `json:"endpointUrl"`
	IsInSecure  bool   `json:"isInSecure"`
	Region      string `json:"region"`
}

type ImageDetailsCi struct {
	ImageDetails []types.ImageDetail `json:"imageDetails"`
	CiPipelineId int                 `json:"ciPipelineId"`
}

func GetResultsAndSaveInFile(accessKey, secretKey, dockerRegistryURL, awsRegion, repositoryName string, lastFetchedTime time.Time, ciPipelineId int) error {
	awsConfig := &AwsBaseConfig{
		AccessKey: accessKey,
		Passkey:   secretKey,
		Region:    awsRegion,
	}

	client, err := GetAwsClientFromCred(awsConfig)
	if err != nil {
		fmt.Println("error in creating client for aws config", "err", err)
		return err
	}
	registryId := bean.ExtractOutRegistryId(dockerRegistryURL)
	allImages, err := GetAllImagesWithMetadata(client, registryId, repositoryName)
	if err != nil {
		fmt.Println("error in getting all images from ecr repo", "err", err, "repoName", repositoryName)
		return err
	}
	var filteredImages []types.ImageDetail
	if lastFetchedTime.IsZero() {
		filteredImages = getLastPushedImages(allImages)
	} else {
		filteredImages = filterAlreadyPresentArtifacts(allImages, lastFetchedTime)
	}

	imageDetailsAgainstCi := &ImageDetailsCi{
		ImageDetails: filteredImages,
		CiPipelineId: ciPipelineId,
	}
	file, err := json.MarshalIndent(imageDetailsAgainstCi, "", " ")
	if err != nil {
		fmt.Println("error in marshalling intend results", "err", err)
		return err
	}
	err = ioutil.WriteFile(bean.FileName, file, bean.PermissionMode)
	if err != nil {
		fmt.Println("error in writing results to json file", "err", err)
		return err
	}
	return nil

}

func GetAwsClientFromCred(ecrBaseConfig *AwsBaseConfig) (*ecr.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ecrBaseConfig.AccessKey, ecrBaseConfig.Passkey, "")))
	if err != nil {
		fmt.Println("error in loading default config from aws ecr credentials", "err", err)
		return nil, err
	}
	cfg.Region = ecrBaseConfig.Region
	// Create ECR client from Config
	svcClient := ecr.NewFromConfig(cfg)

	return svcClient, err
}

// GetAllImagesWithMetadata describe all images present in the repository using next token
func GetAllImagesWithMetadata(client *ecr.Client, registryId, repositoryName string) ([]types.ImageDetail, error) {
	describeImageInput := &ecr.DescribeImagesInput{
		RepositoryName: &repositoryName,
		RegistryId:     &registryId,
	}
	var nextToken *string
	var describeImagesResults []types.ImageDetail
	for {
		if nextToken != nil {
			describeImageInput.NextToken = nextToken
		}
		describeImagesOutput, err := client.DescribeImages(context.Background(), describeImageInput)
		if err != nil {
			fmt.Println("error in describe images from ecr", "err", err, "repoName", repositoryName, "registryId", registryId)
			return nil, err
		}
		describeImagesResults = append(describeImagesResults, describeImagesOutput.ImageDetails...)
		nextToken = describeImagesOutput.NextToken
		if nextToken == nil {
			fmt.Println("no more images are present in the repository")
			break
		}
	}
	return describeImagesResults, nil

}

// return last 5 images in case of no last fetched time
func getLastPushedImages(filterImages []types.ImageDetail) []types.ImageDetail {
	sort.Slice(filterImages, func(i, j int) bool {
		return filterImages[i].ImagePushedAt.After(*filterImages[j].ImagePushedAt)
	})
	if len(filterImages) >= 5 {
		return filterImages[:5]
	}
	return filterImages
}

func filterAlreadyPresentArtifacts(describeImagesResults []types.ImageDetail, lastFetchedTime time.Time) []types.ImageDetail {
	filteredImages := make([]types.ImageDetail, 0)
	for _, image := range describeImagesResults {
		if image.ImagePushedAt.After(lastFetchedTime) {
			filteredImages = append(filteredImages, image)
		}
	}
	return filteredImages
}
