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
	"github.com/caarlos0/env"
	"github.com/tidwall/sjson"
	"io/ioutil"
	"sort"
	"strings"
	"time"
)

type DockerConfiguration struct {
	AccessKey       string `env:"ACCESS_KEY"`
	SecretKey       string `env:"SECRET_KEY"`
	EndPointUrl     string `env:"DOCKER_REGISTRY_URL"`
	AwsRegion       string `env:"AWS_REGION"`
	LastFetchedTime string `env:"LAST_FETCHED_TIME"`
	Repositories    string `env:"REPOSITORY"`
}

func GetDockerConfiguration() (*DockerConfiguration, error) {
	cfg := &DockerConfiguration{}
	err := env.Parse(cfg)
	if err != nil {
		return nil, err
	}
	return cfg, err
}

func main() {

	dockerConfiguration, err := GetDockerConfiguration()
	if err != nil {
		fmt.Println("error in getting docker configuration", "err", err.Error())
		panic("error in getting docker configuration")
	}
	lastFetchedTime, err := parseTime(dockerConfiguration.LastFetchedTime)
	if err != nil {
		fmt.Println("error in parsing last fetched time, using time zero in golang", err)
	}
	repo := strings.Split(dockerConfiguration.Repositories, ",")
	for _, value := range repo {
		err = GetResultsAndSaveInFile(dockerConfiguration.AccessKey, dockerConfiguration.SecretKey, dockerConfiguration.EndPointUrl, dockerConfiguration.AwsRegion, value, lastFetchedTime)
		if err != nil {
			fmt.Println("error in  getting results and saving", "err", err.Error())
			panic("error in  getting results and saving")
		}
	}

}

func parseTime(timeString string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, timeString)
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

type ImageDetailsFromCR struct {
	ImageDetails []types.ImageDetail `json:"imageDetails"`
	Region       string              `json:"region"`
}

// GetResultsAndSaveInFile Polls the Cr and get updated metadata and images from CR and save it to specified file.
func GetResultsAndSaveInFile(accessKey, secretKey, dockerRegistryURL, awsRegion, repositoryName string, lastFetchedTime time.Time) error {
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
		imageDetailsAgainstCi := &ImageDetailsFromCR{
			ImageDetails: filteredImages,
			Region:       awsRegion,
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
	fmt.Println("Polling from container registry succeeded")
	return nil

}

// GetAwsClientFromCred creates service client for ecr operations
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
			fmt.Println("no more images are present in the repository to process")
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

// filterAlreadyPresentArtifacts filter out the images which are before the last fetched time to avoid same images.
func filterAlreadyPresentArtifacts(describeImagesResults []types.ImageDetail, lastFetchedTime time.Time) []types.ImageDetail {
	filteredImages := make([]types.ImageDetail, 0)
	for _, image := range describeImagesResults {
		if image.ImagePushedAt.After(lastFetchedTime) {
			filteredImages = append(filteredImages, image)
		}
	}
	return filteredImages
}
