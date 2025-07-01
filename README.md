# BAMMM: Batch Automatic Magic Multiplexing Mechanism

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-Development-orange.svg)]()

## Overview

BAMMM is an open-source command-line tool that converts batch job specifications between different scheduler formats. It provides a unified intermediate format that can be translated to and from various batch schedulers including:

- **Kubernetes-based**: Armada, Volcano, MCAD, Kueue, YuniKorn
- **HPC**: Slurm, Flux
- **Cloud**: AWS Batch

## Problem Statement

Today's batch computing landscape is fragmented across multiple schedulers, each with their own job specification formats:

- **Slurm**: Shell scripts with `#SBATCH` directives
- **Kubernetes-based**: YAML manifests with scheduler-specific CRDs (Kueue, MCAD, Armada, YuniKorn)
- **AWS Batch**: JSON job definitions
- **Flux**: JSON jobspec format

This fragmentation creates several challenges:
- **Vendor lock-in**: Jobs written for one scheduler can't easily migrate to another
- **Learning curve**: Developers must learn multiple job specification formats
- **Maintenance overhead**: Supporting multiple schedulers requires maintaining separate job definitions
- **Deployment complexity**: Different environments require different job formats

## Solution: Unified Batch Job Converter

BAMMM defines a single, comprehensive YAML specification that captures the common semantics across all major batch schedulers. The tool acts as a converter between different scheduler formats:

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Source        │    │   BAMMM         │    │   Target        │
│   Format        │───▶│   Converter     │───▶│   Format        │
│                 │    │                 │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
                                              ┌─────────────────┐
                                              │   Slurm         │
                                              │   AWS Batch     │
                                              │   Volcano       │
                                              │   YuniKorn      │
                                              │   Flux          │
                                              │   Kueue         │
                                              │   MCAD          │
                                              └─────────────────┘
```

## Key Features

### 🎯 **Universal Compatibility**
- Convert between any supported scheduler formats
- Preserve scheduler-specific features via override blocks
- Maintain job semantics across conversions

### 🔧 **Command-Line Interface**
- Simple CLI for format conversion
- Batch processing of multiple jobs
- Validation of job specifications

## Getting Started

BAMMM is currently in active development. The tool will provide a simple command-line interface for converting between different batch job specification formats.

Example usage will include:
- Converting Slurm scripts to Volcano YAML
- Converting AWS Batch JSON to Slurm scripts
- Validating job specifications
- Supporting all major batch schedulers

## Development Status

### ✅ **Completed**

### 🚧 **In Progress**
- [ ] Unified job specification design
- [ ] Basic converter framework
- [ ] Cross-scheduler feature mapping
- [ ] Slurm to unified format converter
- [ ] Unified format to Slurm converter
- [ ] AWS Batch format converters
- [ ] Volcano format converters

### 📋 **Planned**
- [ ] Flux format support
- [ ] Kueue format support
- [ ] MCAD format support
- [ ] YuniKorn format support
- [ ] Job validation and linting
- [ ] Batch processing capabilities

## Collaboration

This is an active collaboration with:

- AWS
- Nitka Consulting
- Insight Softmax Consulting

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup

Development setup instructions will be provided once the implementation approach is finalized.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Contact

- **Project Lead**: [Your Name]
- **Email**: [your-email@example.com]
- **Slack**: [Slack channel link]
- **Discussions**: [GitHub Discussions](https://github.com/your-org/BAMMM/discussions)

## Acknowledgments

- **Armada Project** - For Kubernetes-native batch scheduling inspiration
- **Volcano** - For gang scheduling and advanced Kubernetes job patterns
- **Slurm** - For HPC scheduling best practices
- **AWS Batch** - For cloud-native batch computing patterns
- **Flux** - For modern HPC job specification design
- **Kueue** - For Kubernetes-native queuing patterns

---

**BAMMM**: Making batch computing portable across all platforms through intelligent format conversion.
