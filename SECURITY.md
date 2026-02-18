# Security Policy

## Supported Versions

We take security vulnerabilities seriously and will address them promptly. The following versions of KubeNexus Scheduler are currently supported with security updates:

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

We appreciate responsible disclosure of security vulnerabilities. If you discover a security issue in KubeNexus Scheduler, please report it by following these steps:

### How to Report

1. **DO NOT** create a public GitHub issue for security vulnerabilities.
2. Email your findings to the maintainers at **security@kubenexus.io** (or create a private security advisory on GitHub).
3. Include the following information in your report:
   - Description of the vulnerability
   - Steps to reproduce the issue
   - Potential impact
   - Suggested fix (if available)
   - Your contact information

### What to Expect

- **Acknowledgment**: We will acknowledge receipt of your report within 48 hours.
- **Initial Assessment**: Within 5 business days, we will provide an initial assessment of the vulnerability.
- **Updates**: We will keep you informed of our progress throughout the investigation and resolution process.
- **Resolution**: Once the vulnerability is fixed, we will:
  - Release a security patch
  - Publish a security advisory
  - Credit you for the discovery (unless you prefer to remain anonymous)

### Security Update Process

1. Security vulnerabilities are privately fixed and reviewed.
2. A new version is released with the fix.
3. A security advisory is published with:
   - Description of the vulnerability
   - Affected versions
   - Fixed version
   - Workarounds (if applicable)
   - Credit to the reporter

## Security Best Practices

When deploying KubeNexus Scheduler, we recommend:

1. **RBAC Configuration**: Use minimal required permissions for the scheduler service account.
2. **Network Policies**: Restrict network access to the scheduler components.
3. **Resource Limits**: Set appropriate resource limits and quotas.
4. **Regular Updates**: Keep KubeNexus Scheduler and its dependencies up to date.
5. **Monitoring**: Enable audit logging and monitor scheduler behavior.
6. **Secrets Management**: Use Kubernetes Secrets or external secret management solutions.

## Known Security Considerations

- The scheduler requires elevated Kubernetes RBAC permissions to function properly.
- ResourceReservation CRDs may contain sensitive information about workload resource requirements.
- Gang scheduling decisions could be influenced by malicious pod configurations if not properly validated.

## Vulnerability Disclosure Timeline

We aim to:
- Provide an initial response within 48 hours
- Publish a fix within 30 days for high-severity issues
- Publish a fix within 90 days for moderate-severity issues
- Coordinate disclosure with reporters

## Contact

For security-related inquiries:
- Email: security@kubenexus.io
- GitHub Security Advisories: [Create a private security advisory](https://github.com/YOUR_USERNAME/kubenexus-scheduler/security/advisories/new)

For general questions, please use GitHub Discussions or Issues (non-security topics only).
