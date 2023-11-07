package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/template"

	"github.com/alecthomas/kong"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"k8s.io/utils/ptr"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane/crossplane/cmd/crank/beta/importer/internal/aws"
)

// awsCmd arguments and flags for aws subcommand.
type awsCmd struct {
	Flags `embed:""`

	// Provider-specific flags
	Region string            `help:"AWS region to use for AWS resources."`
	Tags   map[string]string `help:"Tags to apply to AWS resources."`
}

func (c *awsCmd) Help() string {
	return `
This command generates Crossplane resource manifests for the existing AWS
resources.

Examples:
  # Generate Crossplane resource manifests for the existing AWS VPCs and Subnets
  # in us-east-1 region, with tags key1=value1 and key2=value2.
  crossplane beta import aws --resources=vpc,subnet --region us-east-1 --tags "key1=value1,key2=value2"

  # Output to a specific file instead of stdout.
  crossplane beta import aws -o output.yaml --resources=vpc,subnet --region us-east-1 --tags "key1=value1,key2=value2"
`
}

// Run import for aws resources.
func (c *awsCmd) Run(k *kong.Context, _ logging.Logger) error {
	// TODO
	ctx := context.Background()
	var output io.Writer
	switch n := c.Output; n {
	case "-":
		output = k.Stdout
	default:
		f, err := os.OpenFile(n, os.O_CREATE, 0600) //nolint:gosec // that's actually what we want
		if err != nil {
			return errors.Wrap(err, "opening output file")
		}
		defer func() {
			_ = f.Close()
		}()
		output = f

	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "loading aws configuration")
	}

	var resources []interface{}

	ec2Client := ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		o.Region = c.Region
	})

	// TODO dedup resources, should be unique

	for _, resource := range c.Resources {
		switch strings.ToLower(resource) {
		case "vpc":
			var filters []types.Filter
			for k, v := range c.Tags {
				filters = append(filters, types.Filter{Name: ptr.To(fmt.Sprintf("tag:%s", k)), Values: []string{v}})
			}
			input := &ec2.DescribeVpcsInput{
				Filters:    filters,
				MaxResults: ptr.To[int32](100),
			}
			for {
				resp, err := ec2Client.DescribeVpcs(ctx, input)
				if err != nil {
					return errors.Wrap(err, "getting vpcs")
				}
				for i := range resp.Vpcs {
					resources = append(resources, resp.Vpcs[i])
				}
				if resp.NextToken == nil {
					break
				}
				input.NextToken = resp.NextToken
			}
		case "subnet":
			var filters []types.Filter
			for k, v := range c.Tags {
				filters = append(filters, types.Filter{Name: ptr.To(fmt.Sprintf("tag:%s", k)), Values: []string{v}})
			}
			input := &ec2.DescribeSubnetsInput{
				Filters:    filters,
				MaxResults: ptr.To[int32](100),
			}
			for {
				resp, err := ec2Client.DescribeSubnets(ctx, input)
				if err != nil {
					return errors.Wrap(err, "getting vpcs")
				}
				for i := range resp.Subnets {
					resources = append(resources, resp.Subnets[i])
				}
				if resp.NextToken == nil {
					break
				}
				input.NextToken = resp.NextToken
			}
		default:
			return errors.Errorf("Unknown resource type: %s", resource)
		}
	}

	tmpls := template.Must(aws.GetTemplates())

	for _, resource := range resources {
		s := &strings.Builder{}
		tmplName := fmt.Sprintf("%s.yaml.tmpl", strings.ToLower(reflect.TypeOf(resource).Name()))
		if err := tmpls.ExecuteTemplate(
			s,
			tmplName,
			map[string]interface{}{
				"Object": resource,
			}); err != nil {
			return errors.Wrapf(err, "unable to render template: %s", tmplName)
		}
		out := s.String()
		if !strings.HasPrefix(out, "---") {
			fmt.Fprintln(output, "---")
		}
		fmt.Fprintln(output, out)
	}

	return nil
}
