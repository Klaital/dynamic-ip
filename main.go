package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	yaml "gopkg.in/yaml.v3"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
)

type Config struct {
	RemoteIpSourceUri string `yaml:"remote_ip_source_uri"`
	HostedZones       []struct {
		ID      string   `yaml:"id"`
		Domains []string `yaml:"domains"`
	} `yaml:"hosted_zones"`
}

func main() {
	ctx := context.Background()
	// Load the config file
	configPath := "dynamic-ip.yaml"
	Config := Config{}
	file, err := os.ReadFile(configPath)
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(file, &Config)
	if err != nil {
		panic(err)
	}

	// Set up the Route53 client
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-west-2"))
	if err != nil {
		panic(err)
	}
	r53Client := route53.NewFromConfig(awsConfig)

	// Fetch our public IP address from the remote tool
	resp, err := http.Get(Config.RemoteIpSourceUri)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	remoteIp := string(b)
	fmt.Printf("Remote IP: %s\n", remoteIp)
	// Update Route53 records
	for _, zone := range Config.HostedZones {
		fmt.Printf("Checking HostedZone %+v for updates\n", zone)
		r53Resp, err := r53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
			HostedZoneId: aws.String(zone.ID),
		})
		if err != nil {
			log.Fatalf("Failed to list records for zone %s: %s", zone.ID, err.Error())
		}
		changes := make([]types.Change, 0)
		for _, record := range r53Resp.ResourceRecordSets {
			if record.Type != types.RRTypeA {
				continue
			}
			if !slices.Contains(zone.Domains, aws.ToString(record.Name)) {
				continue
			}
			if aws.ToString(record.ResourceRecords[0].Value) == remoteIp {
				log.Printf("Record %s already up to date\n", aws.ToString(record.Name))
				continue
			}
			log.Printf("Updating record %s\n", aws.ToString(record.Name))
			changes = append(changes, types.Change{
				Action: types.ChangeActionUpsert,
				ResourceRecordSet: &types.ResourceRecordSet{
					Name: record.Name,
					Type: record.Type,
					TTL:  record.TTL,
					ResourceRecords: []types.ResourceRecord{
						{
							Value: aws.String(remoteIp),
						},
					},
				},
			})
		}

		if len(changes) == 0 {
			continue
		}
		_, err = r53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(zone.ID),
			ChangeBatch: &types.ChangeBatch{
				Changes: changes,
			},
		})
		if err != nil {
			log.Fatalf("Failed to update records for zone %s: %s", zone.ID, err.Error())
		}
	}
}
