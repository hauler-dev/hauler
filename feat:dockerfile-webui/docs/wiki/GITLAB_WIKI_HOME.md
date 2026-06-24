# Hauler UI Wiki - Home

Welcome to the Hauler UI Wiki! This comprehensive guide will help you get the most out of Hauler UI.

---

## 📚 Quick Navigation

### Getting Started
- **[Installation Guide](Installation-Guide)** - Step-by-step installation
- **[Quick Start](Quick-Start)** - Get running in 5 minutes
- **[Configuration](Configuration)** - Configure Hauler UI for your environment

### User Guides
- **[Repository Management](Repository-Management)** - Managing Helm repositories
- **[Chart Operations](Chart-Operations)** - Adding and managing charts
- **[Image Operations](Image-Operations)** - Working with container images
- **[Store Management](Store-Management)** - Store operations and maintenance
- **[Registry Operations](Registry-Operations)** - Pushing to private registries

### Advanced Topics
- **[Signature Verification](Signature-Verification)** - Cosign integration
- **[Platform Selection](Platform-Selection)** - Multi-architecture support
- **[Rewrite Paths](Rewrite-Paths)** - Customizing registry paths
- **[TLS Configuration](TLS-Configuration)** - Secure registry and fileserver
- **[Airgap Deployment](Airgap-Deployment)** - Offline installation

### Development
- **[Architecture](Architecture)** - System architecture and design
- **[API Reference](API-Reference)** - Complete API documentation
- **[Development Setup](Development-Setup)** - Setting up dev environment
- **[Contributing](Contributing)** - How to contribute

### Operations
- **[Deployment](Deployment)** - Production deployment guide
- **[Security](Security)** - Security best practices
- **[Troubleshooting](Troubleshooting)** - Common issues and solutions
- **[Performance Tuning](Performance-Tuning)** - Optimization guide

### Reference
- **[CLI Flag Coverage](CLI-Flag-Coverage)** - Complete flag mapping
- **[FAQ](FAQ)** - Frequently asked questions
- **[Glossary](Glossary)** - Terms and definitions
- **[Release Notes](Release-Notes)** - Version history

---

## 🎯 What is Hauler UI?

Hauler UI is a modern web interface for [Rancher Government Hauler](https://hauler.dev), providing:

- ✅ **100% Feature Parity** with Hauler CLI (72/72 flags)
- 🎨 **Interactive Content Selection** - Browse and select charts/images visually
- 🔒 **Airgap Ready** - All assets bundled, no external dependencies
- 🐳 **Docker Native** - Single container deployment
- 📦 **Repository Management** - Add, browse, and manage Helm repositories
- 🔐 **Security Focused** - Obfuscated JavaScript, security hardening

---

## 🚀 Quick Start

```bash
# Pull and run
docker run -d -p 8080:8080 -v hauler-data:/data hauler-ui:latest

# Access UI
open http://localhost:8080
```

See the **[Quick Start Guide](Quick-Start)** for detailed instructions.

---

## 🤖 Built with Agentic Prompt Engineering

This project was developed using advanced AI-assisted development methodologies:

- **Product Manager Agent** - Requirements analysis
- **Software Development Manager Agent** - Architecture and planning
- **Senior Developer Agents** - Implementation
- **QA Agent** - Testing and quality assurance
- **Security Agent** - Security review and hardening
- **Technical Writer Agent** - Documentation

Learn more in the **[Agentic Development](Agentic-Development)** page.

---

## 📊 Feature Highlights

### Interactive Chart Browser
Browse Helm repositories visually, select multiple charts, and add them to your store with one click.

### Visual Manifest Builder
Build Hauler manifests visually without writing YAML manually.

### Real-time Logs
Watch Hauler commands execute in real-time via WebSocket connection.

### Complete Flag Coverage
Every Hauler CLI flag is available in the UI with the same functionality.

---

## 🔒 Security

**Current Version:** v3.3.5  
**Security Status:** 🟡 Hardening in Progress

See **[Security Guide](Security)** for:
- Current security status
- Known vulnerabilities
- Remediation plan
- Best practices

**Target:** v3.4.0 (Security Hardened Release)

---

## 📖 Documentation Structure

```
Wiki Home (You are here)
├── Getting Started/
│   ├── Installation Guide
│   ├── Quick Start
│   └── Configuration
├── User Guides/
│   ├── Repository Management
│   ├── Chart Operations
│   ├── Image Operations
│   ├── Store Management
│   └── Registry Operations
├── Advanced Topics/
│   ├── Signature Verification
│   ├── Platform Selection
│   ├── Rewrite Paths
│   ├── TLS Configuration
│   └── Airgap Deployment
├── Development/
│   ├── Architecture
│   ├── API Reference
│   ├── Development Setup
│   └── Contributing
├── Operations/
│   ├── Deployment
│   ├── Security
│   ├── Troubleshooting
│   └── Performance Tuning
└── Reference/
    ├── CLI Flag Coverage
    ├── FAQ
    ├── Glossary
    └── Release Notes
```

---

## 🆘 Need Help?

- **Issues:** [Report a bug](../../issues/new?template=bug_report)
- **Feature Requests:** [Request a feature](../../issues/new?template=feature_request)
- **Questions:** [Ask in Discussions](../../discussions)
- **Hauler Docs:** [https://hauler.dev](https://hauler.dev)

---

## 🗺️ Roadmap

### v3.4.0 - Security Hardened (Q1 2026)
- Input sanitization
- Credential encryption
- Authentication system
- Security scan passing

### v3.5.0 - Enhanced Features (Q2 2026)
- RBAC
- Audit logging
- Metrics and monitoring

### v4.0.0 - Enterprise Ready (Q3 2026)
- LDAP/SAML integration
- High availability
- Advanced reporting

---

## 📝 Recent Updates

- **2026-01-22:** v3.3.5 released with JavaScript obfuscation
- **2026-01-22:** Security scan completed, remediation plan created
- **2026-01-22:** Complete documentation overhaul
- **2026-01-20:** 100% CLI flag coverage achieved
- **2026-01-18:** Airgap readiness confirmed

---

## 🤝 Contributing

We welcome contributions! See the **[Contributing Guide](Contributing)** for:
- Development workflow
- Code standards
- Testing requirements
- Documentation guidelines

---

## 📄 License

Apache License 2.0 - See [LICENSE](../../blob/main/LICENSE)

---

**Last Updated:** 2026-01-22  
**Wiki Version:** 1.0  
**Maintainers:** Hauler UI Development Team

---

**Built with ❤️ using Agentic Prompt Engineering**
