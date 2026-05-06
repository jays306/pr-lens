#!/usr/bin/env bash
# Deploy ECS cluster "mickey" and Fargate service "pr-lens" (see ecs-mickey.yaml).
# Prereqs: AWS CLI, credentials, region us-east-1, image in ECR.
# Default profile: vibe-sandbox (override with AWS_PROFILE=...).

set -euo pipefail

export AWS_PROFILE="${AWS_PROFILE:-vibe-sandbox}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REGION="${AWS_REGION:-us-east-1}"
STACK_NAME="${STACK_NAME:-mickey-ecs-pr-lens-v2}"
TEMPLATE="${ROOT}/infra/ecs-mickey.yaml"

VPC_ID="${VPC_ID:-}"
if [[ -z "${VPC_ID}" ]]; then
  VPC_ID="$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true --query 'Vpcs[0].VpcId' --output text --region "${REGION}")"
fi
if [[ -z "${VPC_ID}" || "${VPC_ID}" == "None" ]]; then
  echo "No default VPC found. Set VPC_ID to your VPC id." >&2
  exit 1
fi

# Two subnets in different AZs (ALB requires this). Prefer private subnets; fall back to any.
if [[ -z "${SUBNET_OVERRIDES:-}" ]]; then
  SUBNET_JSON="$(aws ec2 describe-subnets \
    --filters "Name=vpc-id,Values=${VPC_ID}" "Name=tag:Name,Values=*private*" \
    --query 'Subnets[*].{id:SubnetId,az:AvailabilityZone}' --output json --region "${REGION}")"
  if [[ -z "${SUBNET_JSON}" || "${SUBNET_JSON}" == "[]" ]]; then
    SUBNET_JSON="$(aws ec2 describe-subnets \
      --filters "Name=vpc-id,Values=${VPC_ID}" \
      --query 'Subnets[*].{id:SubnetId,az:AvailabilityZone}' --output json --region "${REGION}")"
  fi
  # Pick one subnet per AZ
  FIRST="$(echo "${SUBNET_JSON}" | python3 -c "
import json,sys
subnets=json.load(sys.stdin)
seen={}
for s in subnets:
  if s['az'] not in seen:
    seen[s['az']]=s['id']
  if len(seen)==2: break
print(' '.join(list(seen.values())))
")"
  SECOND="$(echo "${FIRST}" | awk '{print $2}')"
  FIRST="$(echo "${FIRST}" | awk '{print $1}')"
  if [[ -z "${FIRST}" || -z "${SECOND}" ]]; then
    echo "Need at least two subnets in different AZs in VPC ${VPC_ID}." >&2
    exit 1
  fi
  SUBNET_OVERRIDES="${FIRST},${SECOND}"
fi

# Internal ALB mickey-sandbox (private IPs only); default same subnets as ECS tasks.
ALB_SUBNET_OVERRIDES="${ALB_SUBNET_OVERRIDES:-${SUBNET_OVERRIDES}}"
if [[ -z "${ALB_SUBNET_OVERRIDES}" ]]; then
  echo "Set ALB_SUBNET_OVERRIDES to two subnet IDs for the internal ALB." >&2
  exit 1
fi

VPC_CIDR="$(aws ec2 describe-vpcs --vpc-ids "${VPC_ID}" --query 'Vpcs[0].CidrBlock' --output text --region "${REGION}")"
INTERNAL_ALB_CLIENT_CIDR="${INTERNAL_ALB_CLIENT_CIDR:-${VPC_CIDR}}"
if [[ -z "${INTERNAL_ALB_CLIENT_CIDR}" || "${INTERNAL_ALB_CLIENT_CIDR}" == "None" ]]; then
  echo "Could not read VPC CIDR for ${VPC_ID}. Set INTERNAL_ALB_CLIENT_CIDR (e.g. 10.0.0.0/16)." >&2
  exit 1
fi

SECRET_ARN="${SECRET_ARN:-arn:aws:secretsmanager:us-east-1:200033215230:secret:mickey-LWj9eF}"
IMAGE_URI="${IMAGE_URI:-200033215230.dkr.ecr.us-east-1.amazonaws.com/mickey/pr-lens:latest}"
# X86_64 matches linux/amd64 images built with `docker buildx build --platform linux/amd64`.
TASK_CPU_ARCH="${TASK_CPU_ARCH:-X86_64}"
CERTIFICATE_ARN="${CERTIFICATE_ARN:-arn:aws:acm:us-east-1:200033215230:certificate/f9c19a5e-0b28-4270-8048-282595aa2d67}"

echo "Profile:       ${AWS_PROFILE}"
echo "Region:        ${REGION}"
echo "Stack:         ${STACK_NAME}"
echo "VPC:           ${VPC_ID}"
echo "Task subnets:  ${SUBNET_OVERRIDES}"
echo "ALB subnets:   ${ALB_SUBNET_OVERRIDES} (internal ALB)"
echo "ALB client:    ${INTERNAL_ALB_CLIENT_CIDR}"
echo "Secret ARN:    ${SECRET_ARN}"
echo "Image:         ${IMAGE_URI}"
echo "Task CPU:      ${TASK_CPU_ARCH}"
echo "ACM cert:      ${CERTIFICATE_ARN}"
echo
echo "Env injected by task definition:"
echo "  CLAUDE_CODE_USE_BEDROCK=1  AWS_REGION=us-east-1"
echo "  ANTHROPIC_MODEL=anthropic.claude-opus-4-7"
echo "  AGENT_MAX_TURNS=40  AGENT_SPECIALIST_MAX_TURNS=30"
echo "  CLONE_REPOS=true  PORT=9000"
echo "Secrets from ${SECRET_ARN}:"
echo "  AWS_BEARER_TOKEN_BEDROCK ← BEDROCK_API_KEY"
echo "  GITHUB_TOKEN             ← GITHUB_TOKEN"
echo

aws cloudformation deploy \
  --region "${REGION}" \
  --stack-name "${STACK_NAME}" \
  --template-file "${TEMPLATE}" \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    "VpcId=${VPC_ID}" \
    "SubnetIds=${SUBNET_OVERRIDES}" \
    "SecretArn=${SECRET_ARN}" \
    "EcrImageUri=${IMAGE_URI}" \
    "TaskCpuArchitecture=${TASK_CPU_ARCH}" \
    "AlbSubnetIds=${ALB_SUBNET_OVERRIDES}" \
    "InternalAlbClientCidr=${INTERNAL_ALB_CLIENT_CIDR}" \
    "CertificateArn=${CERTIFICATE_ARN}"

echo
echo "Done. Internal ALB (private DNS only) → tasks. Point private DNS / split-horizon at MickeySandboxAlbDnsName for VPC clients."
echo "  aws cloudformation describe-stacks --stack-name ${STACK_NAME} --region ${REGION} --profile ${AWS_PROFILE} --query 'Stacks[0].Outputs'"
echo "  aws ecs list-tasks --cluster mickey --service-name pr-lens --region ${REGION} --profile ${AWS_PROFILE}"
