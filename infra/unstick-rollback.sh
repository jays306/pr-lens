#!/usr/bin/env bash
# When rollback cleanup fails deleting VPC endpoints (ec2:DeleteVpcEndpoints denied), CloudFormation
# can sit in UPDATE_ROLLBACK_FAILED or finish cleanup with stray resources.
#
# - If status is UPDATE_ROLLBACK_FAILED or UPDATE_ROLLBACK_IN_PROGRESS: run this script (skip deletes).
# - If stuck in UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS: wait, or ask an admin to delete the
#   VPC endpoints in EC2 (see stack events) so cleanup can finish, OR wait for UPDATE_ROLLBACK_FAILED
#   then run this script.
# After UPDATE_ROLLBACK_COMPLETE: deploy the current ecs-mickey.yaml (no VPC endpoints in template).

set -euo pipefail
export AWS_PROFILE="${AWS_PROFILE:-vibe-sandbox}"
REGION="${AWS_REGION:-us-east-1}"
STACK="${STACK_NAME:-mickey-ecs-pr-lens}"

STATUS="$(aws cloudformation describe-stacks --region "${REGION}" --stack-name "${STACK}" \
  --query 'Stacks[0].StackStatus' --output text 2>/dev/null || echo UNKNOWN)"
echo "Current stack status: ${STATUS}"

case "${STATUS}" in
  UPDATE_ROLLBACK_FAILED|UPDATE_ROLLBACK_IN_PROGRESS)
    echo "Continuing rollback with VPC endpoint deletes skipped..."
    aws cloudformation continue-update-rollback \
      --region "${REGION}" \
      --stack-name "${STACK}" \
      --resources-to-skip \
        VpcEndpointSecurityGroup \
        SecretsManagerVpcEndpoint \
        EcrApiVpcEndpoint \
        EcrDkrVpcEndpoint \
        LogsVpcEndpoint \
        KmsVpcEndpoint
    echo "Waiting for rollback to complete..."
    aws cloudformation wait stack-rollback-complete --region "${REGION}" --stack-name "${STACK}"
    ;;
  UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS)
    echo "Cleanup still running. You cannot use continue-update-rollback yet." >&2
    echo "Wait, or have an admin delete the failing VPC endpoints (see: aws cloudformation describe-stack-events)." >&2
    exit 1
    ;;
  UPDATE_ROLLBACK_COMPLETE)
    echo "Stack is already stable. Run: ./infra/deploy-ecs.sh"
    ;;
  *)
    echo "Unexpected status. Check: aws cloudformation describe-stack-events --stack-name ${STACK} --region ${REGION}" >&2
    exit 1
    ;;
esac

aws cloudformation describe-stacks --region "${REGION}" --stack-name "${STACK}" \
  --query 'Stacks[0].StackStatus' --output text
