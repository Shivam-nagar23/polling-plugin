package bean

import (
	"fmt"
	"os"
	"strings"
)

const (
	PermissionMode = 0644
	FileName       = "results.json"
)

func ExtractOutRegistryId(hostUrl string) string {
	res := strings.Split(hostUrl, ".")
	return res[0]

}
func CheckFileExistsOrCreate(filename string) error {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		_, err := os.Create(filename)
		if err != nil {
			return err
		}
	}
	return nil
}

// /445808685819.dkr.ecr.us-east-2.amazonaws.com/devtron/html-ecr:cf50e450-125-588///Sample Image for reference
func GetHostUrlForEcr(registryId, region string) string {
	return fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", registryId, region)
}
