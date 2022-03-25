// Code generated by smithy-go-codegen DO NOT EDIT.

package ec2

import (
	"context"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Describes the default credit option for CPU usage of a burstable performance
// instance family. For more information, see Burstable performance instances
// (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/burstable-performance-instances.html)
// in the Amazon EC2 User Guide.
func (c *Client) GetDefaultCreditSpecification(ctx context.Context, params *GetDefaultCreditSpecificationInput, optFns ...func(*Options)) (*GetDefaultCreditSpecificationOutput, error) {
	if params == nil {
		params = &GetDefaultCreditSpecificationInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "GetDefaultCreditSpecification", params, optFns, c.addOperationGetDefaultCreditSpecificationMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*GetDefaultCreditSpecificationOutput)
	out.ResultMetadata = metadata
	return out, nil
}

type GetDefaultCreditSpecificationInput struct {

	// The instance family.
	//
	// This member is required.
	InstanceFamily types.UnlimitedSupportedInstanceFamily

	// Checks whether you have the required permissions for the action, without
	// actually making the request, and provides an error response. If you have the
	// required permissions, the error response is DryRunOperation. Otherwise, it is
	// UnauthorizedOperation.
	DryRun *bool

	noSmithyDocumentSerde
}

type GetDefaultCreditSpecificationOutput struct {

	// The default credit option for CPU usage of the instance family.
	InstanceFamilyCreditSpecification *types.InstanceFamilyCreditSpecification

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata

	noSmithyDocumentSerde
}

func (c *Client) addOperationGetDefaultCreditSpecificationMiddlewares(stack *middleware.Stack, options Options) (err error) {
	err = stack.Serialize.Add(&awsEc2query_serializeOpGetDefaultCreditSpecification{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsEc2query_deserializeOpGetDefaultCreditSpecification{}, middleware.After)
	if err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddClientRequestIDMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddComputeContentLengthMiddleware(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = v4.AddComputePayloadSHA256Middleware(stack); err != nil {
		return err
	}
	if err = addRetryMiddlewares(stack, options); err != nil {
		return err
	}
	if err = addHTTPSignerV4Middleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = awsmiddleware.AddRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = addOpGetDefaultCreditSpecificationValidationMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opGetDefaultCreditSpecification(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	return nil
}

func newServiceMetadataMiddleware_opGetDefaultCreditSpecification(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		SigningName:   "ec2",
		OperationName: "GetDefaultCreditSpecification",
	}
}