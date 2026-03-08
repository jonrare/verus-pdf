# VerusPDF — Deploy Guide

## Architecture

```
veruspdf.com  ──→  CloudFront  ──→  S3 (landing page)
                       │
downloads.veruspdf.com ──→  S3 (build artifacts)
                       │
                  /latest/*   → always points to newest build
                  /v1.0.0/*   → immutable versioned copies
```

One CloudFront distribution serves both origins. Path-based routing
sends `/latest/*` and `/v*/*` to the downloads bucket, everything
else to the site bucket.

---

## First-Time Setup (10 minutes)

### 1. Register domain + create hosted zone

If you haven't already, register `veruspdf.com` in Route 53 (or
transfer it). Note the **Hosted Zone ID** — you'll need it below.

### 2. Request an ACM certificate

**Must be in us-east-1** (CloudFront requirement).

```bash
aws acm request-certificate \
  --region us-east-1 \
  --domain-name veruspdf.com \
  --subject-alternative-names "*.veruspdf.com" \
  --validation-method DNS
```

Go to the ACM console, expand the cert, and click "Create records in
Route 53" to auto-validate. Wait ~2 minutes for it to flip to "Issued."

Note the **Certificate ARN**.

### 3. Deploy the CloudFormation stack

```bash
aws cloudformation deploy \
  --region us-east-1 \
  --template-file infra/cloudformation.yml \
  --stack-name veruspdf-infra \
  --parameter-overrides \
      DomainName=veruspdf.com \
      CertificateArn=arn:aws:acm:us-east-1:ACCOUNT:certificate/CERT-ID \
      HostedZoneId=Z0XXXXXXXXXXXXX
```

This creates:
- `veruspdf.com` S3 bucket (landing page)
- `downloads.veruspdf.com` S3 bucket (binaries)
- CloudFront distribution with both origins
- Route 53 A records for `veruspdf.com`, `www.veruspdf.com`, `downloads.veruspdf.com`

Takes ~5 minutes for CloudFront to provision.

### 4. Get the CloudFront Distribution ID

```bash
aws cloudformation describe-stacks \
  --stack-name veruspdf-infra \
  --query "Stacks[0].Outputs[?OutputKey=='CloudFrontDistributionId'].OutputValue" \
  --output text
```

### 5. Set GitHub Secrets

In your GitHub repo → Settings → Secrets and variables → Actions, add:

| Secret                        | Value                              |
|-------------------------------|------------------------------------|
| `AWS_ACCESS_KEY_ID`           | IAM user access key                |
| `AWS_SECRET_ACCESS_KEY`       | IAM user secret key                |
| `CLOUDFRONT_DISTRIBUTION_ID`  | From step 4                        |

The IAM user needs these permissions:
- `s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`, `s3:ListBucket` on both buckets
- `cloudfront:CreateInvalidation` on the distribution

### 6. Upload the landing page (first time)

```bash
aws s3 sync landing/ s3://veruspdf.com/ --delete
```

After this, the GitHub Actions workflow handles everything.

---

## Day-to-Day Usage

### Deploying a new build

1. Go to your repo → Actions → "Build & Deploy VerusPDF"
2. Click "Run workflow"
3. Enter version (e.g. `1.0.0`)
4. Check "Also deploy landing page?" if you changed the HTML
5. Click "Run workflow"

The workflow will:
- Build Windows (.exe), macOS (.dmg), Linux (.AppImage) in parallel
- Upload to `s3://downloads.veruspdf.com/v1.0.0/` (immutable)
- Copy to `s3://downloads.veruspdf.com/latest/` (what the site links to)
- Sync the landing page to `s3://veruspdf.com/`
- Invalidate CloudFront cache
- Create a GitHub Release with the artifacts attached

### Updating just the landing page

Either run the workflow with deploy_site=true, or manually:

```bash
aws s3 sync landing/ s3://veruspdf.com/ --delete
aws cloudfront create-invalidation --distribution-id EXXXXX --paths "/*"
```

---

## Download URLs

The landing page links always point to `/latest/`:

| Platform | URL |
|----------|-----|
| Windows  | `https://downloads.veruspdf.com/latest/VerusPDF-windows-x64.exe` |
| macOS    | `https://downloads.veruspdf.com/latest/VerusPDF-macos-universal.dmg` |
| Linux    | `https://downloads.veruspdf.com/latest/VerusPDF-linux-x64.AppImage` |

Versioned URLs are also available:
- `https://downloads.veruspdf.com/v1.0.0/VerusPDF-windows-x64.exe`

A manifest file at `https://downloads.veruspdf.com/latest/manifest.json`
contains the current version and build date, useful if you later want
the app to check for updates.

---

## Cost Estimate

- **S3**: pennies/month (landing page is ~30KB, binaries are ~20-50MB each)
- **CloudFront**: free tier covers 1TB/month transfer, 10M requests
- **Route 53**: $0.50/month per hosted zone + $0.40/million queries
- **GitHub Actions**: macOS runners use 10x minutes; a full 3-platform build uses ~30 billed minutes

Realistically **under $2/month** unless you get serious download traffic.
