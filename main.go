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
	"github.com/tidwall/sjson"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
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
	repo := strings.Split(repoName, ",")
	for _, value := range repo {
		err = GetResultsAndSaveInFile(accessKey, secretKey, endpointUrl, region, value, lastFetchedTime, ciPipelineId)
		if err != nil {
			fmt.Println("error i  getting results and saving", "err", err.Error())
			continue
		}
	}

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

	fileExist, err := bean.CheckFileExists(bean.FileName)
	if err != nil {
		fmt.Println("error in checking file exist or not", "err", err.Error())
		return err
	}
	if fileExist {
		file, err := ioutil.ReadFile(bean.FileName)
		if err != nil {
			fmt.Println("error in reading file", "err", err.Error())
			return err
		}
		updatedFile := string(file)
		for _, val := range filteredImages {
			updatedFile, err = sjson.Set(updatedFile, "imageDetails.-1", val)
			if err != nil {
				fmt.Println("error in appending in updated file", "err", err.Error())
				return err

			}
		}
		err = bean.WriteToFile(updatedFile, bean.FileName)
		if err != nil {
			fmt.Println("error in writing file", "err", err.Error())
			return err
		}

	} else {
		imageDetailsAgainstCi := &ImageDetailsCi{
			ImageDetails: filteredImages,
			CiPipelineId: ciPipelineId,
		}

		file, err := json.MarshalIndent(imageDetailsAgainstCi, "", " ")
		if err != nil {
			fmt.Println("error in marshalling intend results", "err", err)
			return err
		}
		err = bean.WriteToFile(string(file), bean.FileName)
		if err != nil {
			fmt.Println("error in writing file", "err", err.Error())
			return err
		}
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
