package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// awsOutfileCases are the aws operations whose verb reads like a read and which stream their response
// into the outfile operand they require, so each writes a file at a path the caller picks. They were
// reads before 3.2.2. The list comes from the AWS CLI's service models (awscli 2.36.46): every
// read-prefixed operation whose output payload is a blob, the shape the CLI gives an outfile operand.
var awsOutfileCases = []string{
	"agenttoolkit get-skill-file", "appconfig get-configuration", "appconfig get-hosted-configuration-version",
	"appconfigdata get-latest-configuration", "appsync get-introspection-schema",
	"cloudfront get-connection-function", "cloudfront get-function", "codeguruprofiler get-profile",
	"datazone get-lineage-event", "geo-maps get-glyphs", "geo-maps get-sprites", "geo-maps get-static-map",
	"geo-maps get-style-descriptor", "geo-maps get-tile", "iotwireless get-position-estimate",
	"iotwireless get-resource-position", "kinesis-video-archived-media get-media-for-fragment-list",
	"lakeformation get-work-unit-results", "location get-map-glyphs", "location get-map-sprites",
	"location get-map-style-descriptor", "location get-map-tile", "medialive describe-input-device-thumbnail",
	"medical-imaging get-image-frame", "medical-imaging get-image-set-metadata", "omics get-read-set",
	"omics get-reference", "s3api get-object-annotation", "sagemaker-geospatial get-tile",
	"schemas get-code-binding-source", "tnb get-sol-function-package-content",
	"tnb get-sol-function-package-descriptor", "tnb get-sol-network-package-content",
	"tnb get-sol-network-package-descriptor",
}

// TestAWSOperationsThatWriteTheirOutfileAreMutations: each operation above writes its response to the
// file named last on the line, through the cloud tool and through the shell alike. The same verb in
// another service can be a plain read (lambda get-function, rolesanywhere get-profile,
// ssm-quicksetup get-configuration, appsync get-function), and those stay reads: the operations are
// told apart by service and verb, not by the verb alone.
func TestAWSOperationsThatWriteTheirOutfileAreMutations(t *testing.T) {
	if len(awsOutfileCases) != 34 {
		t.Fatalf("%d operations, want the 34 the CLI's models name", len(awsOutfileCases))
	}
	var cases []hardeningCase
	for _, op := range awsOutfileCases {
		verb := op[strings.Index(op, " ")+1:]
		if got := Cloud("aws", strings.Fields(op+" --id x out.bin")); got.Classification != policy.Mutate ||
			!strings.Contains(got.Reason, "writes a file") {
			t.Errorf("cloud tool: aws %s should be a mutation that writes a file: %+v", op, got)
		}
		cases = append(cases, hardeningCase{"aws " + op + " --id x out.bin", verb})
	}
	checkMutations(t, cases)
	checkReads(t, []string{"aws lambda get-function --function-name f",
		"aws rolesanywhere get-profile --profile-id p", "aws ssm-quicksetup get-configuration --configuration-id c",
		"aws appsync get-function --api-id a --function-id f", "aws omics get-configuration --name n",
		"aws lightsail get-profile"})
}
