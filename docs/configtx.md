# configtx.yaml & configtxgen in Fabric-X

## Table of Contents

1. [Overview](#1-overview)
2. [Fabric-X vs Legacy Fabric](#2-fabric-x-vs-legacy-fabric)
3. [What is configtx.yaml?](#3-what-is-configtxyaml)
4. [What is configtxgen?](#4-what-is-configtxgen)
5. [The Big Picture: From YAML to Running Network](#5-the-big-picture-from-yaml-to-running-network)
6. [configtx.yaml Section-by-Section Reference](#6-configtxyaml-section-by-section-reference)
   - [Organizations](#61-organizations)
   - [Capabilities](#62-capabilities)
   - [Application](#63-application)
   - [Orderer](#64-orderer)
   - [Channel](#65-channel)
   - [Profiles](#66-profiles)
7. [Arma Consensus: The Fabric-X Ordering Architecture](#7-arma-consensus-the-fabric-x-ordering-architecture)
8. [Orderer Endpoints with Party IDs and Separated APIs](#8-orderer-endpoints-with-party-ids-and-separated-apis)
9. [Policies Deep Dive](#9-policies-deep-dive)
10. [How configtxgen Generates the Genesis Block](#10-how-configtxgen-generates-the-genesis-block)
11. [Genesis Block Internal Structure](#11-genesis-block-internal-structure)
12. [The Network Configuration Hierarchy](#12-the-network-configuration-hierarchy)
13. [configtxgen Command Reference](#13-configtxgen-command-reference)
14. [Step-by-Step Usage Examples](#14-step-by-step-usage-examples)
15. [Troubleshooting](#15-troubleshooting)

---

## 1. Overview

Fabric-X is a next-generation blockchain platform that evolved from Hyperledger
Fabric. Unlike legacy Fabric which was built around a multi-channel model,
**Fabric-X operates on a single-network model** -- one genesis block defines
the entire network topology, its participants, the consensus protocol, and all
governing policies.

Two key pieces work together to bootstrap a Fabric-X network:

```
+-------------------+          +------------------+          +-------------------+
|                   |          |                  |          |                   |
|  configtx.yaml    |  ---->  |   configtxgen    |  ---->   | Genesis Block     |
|  (network config  |          |   (CLI tool)     |          | (defines the      |
|   in human-       |          |                  |          |  entire network   |
|   readable YAML)  |          |                  |          |  topology)        |
|                   |          |                  |          |                   |
+-------------------+          +------------------+          +-------------------+
```

- **configtx.yaml** -- Defines all organizations, their endpoints, the ordering
  service configuration, access policies, and capabilities.
- **configtxgen** -- Reads configtx.yaml and produces a binary genesis block
  that bootstraps every node in the Fabric-X network.

---

## 2. Fabric-X vs Legacy Fabric

Fabric-X fundamentally changes the architecture from legacy Fabric. If you are
coming from Fabric, this section clarifies what has changed.

```
+-------------------------------------------------------------------+
|              LEGACY FABRIC              |        FABRIC-X          |
+=========================================+=========================+
|  Multi-channel: each orderer can host   | Single network: ONE     |
|  many isolated channels, each with its  | genesis block defines   |
|  own config and ledger                  | the ENTIRE network and  |
|                                         | all its participants    |
+-----------------------------------------+-------------------------+
|  System channel required for orderer    | No system channel.      |
|  bootstrap and dynamic channel creation | No dynamic channel      |
|                                         | creation.               |
+-----------------------------------------+-------------------------+
|  Consortiums define which orgs can      | No consortiums.         |
|  create channels together               | All orgs are defined    |
|                                         | upfront in the genesis. |
+-----------------------------------------+-------------------------+
|  Channel creation transactions used     | Not applicable.         |
|  to spawn new channels dynamically      | The network IS the      |
|                                         | single topology.        |
+-----------------------------------------+-------------------------+
|  Consensus: etcdraft (Raft), BFT        | Consensus: Arma         |
|  Single-role orderer nodes              | Decomposed architecture:|
|                                         | Router, Batcher,        |
|                                         | Consenter, Assembler    |
+-----------------------------------------+-------------------------+
|  One orderer endpoint per node          | Separated endpoints:    |
|  (handles both broadcast & deliver)     | distinct broadcast and  |
|                                         | deliver endpoints per   |
|                                         | party, with party IDs   |
+-----------------------------------------+-------------------------+
|  No meta-namespace concept              | Namespaces are created  |
|                                         | via lifecycle endorse-  |
|                                         | ment policy (MAJORITY   |
|                                         | Endorsement)            |
+-----------------------------------------+-------------------------+
|  Simple block committer on peers        | Scalable committer with |
|                                         | views, isolation levels |
|                                         | (SERIALIZABLE, etc.)    |
+-----------------------------------------+-------------------------+
```

**What this means for you as an operator:**

```
Legacy Fabric workflow:                 Fabric-X workflow:
======================                  ==================

1. Generate crypto                      1. Generate crypto
2. Write configtx.yaml                  2. Write configtx.yaml
3. Generate orderer genesis block       3. Generate genesis block
4. Boot orderers with genesis block        (defines entire network)
5. Create channel creation tx           4. Boot ALL nodes with genesis block
6. Submit tx to create a channel           (orderers + peers)
7. Join peers to the channel            5. Network is running
8. Repeat 5-7 for each channel         (no channel creation step)
```

---

## 3. What is configtx.yaml?

`configtx.yaml` is the single source of truth for the Fabric-X network
configuration. It contains **six top-level sections**:

```
configtx.yaml
|
+-- Organizations       Who participates in the network
|                       (MSP identity, policies, orderer endpoints)
|
+-- Capabilities        Feature gates for binary compatibility
|
+-- Application         Configuration for the peer/committer side
|                       (ACLs, policies, lifecycle endorsement)
|
+-- Orderer             Configuration for the ordering service
|                       (Arma consensus, batch parameters, consenters)
|
+-- Channel             Top-level network policies and capabilities
|
+-- Profiles            Named configurations that combine the above
|                       (this is what configtxgen reads)
```

YAML anchors (`&name`) and merge keys (`<<: *name`) are used extensively
to define reusable templates in the top-level sections and reference them
in Profiles.

---

## 4. What is configtxgen?

`configtxgen` is the CLI tool that transforms configtx.yaml into a binary
genesis block.

```
+------------------------------------------------------------------+
|                        configtxgen                               |
|                                                                  |
|  Inputs:                                                         |
|    - configtx.yaml (via -configPath or FABRIC_CFG_PATH env var) |
|    - A profile name (-profile)                                   |
|    - A network/channel ID (-channelID)                           |
|                                                                  |
|  Primary output:                                                 |
|    - Genesis block (-outputBlock)                                |
|                                                                  |
|  Utility outputs:                                                |
|    - Inspect a block as JSON (-inspectBlock)                     |
|    - Print an org definition as JSON (-printOrg)                 |
+------------------------------------------------------------------+
```

---

## 5. The Big Picture: From YAML to Running Network

```
                           YOU (the network operator)
                                |
                                v
                     +----------------------+
                     |  1. Generate crypto  |
                     |  material per org    |
                     |  (Fabric CA or       |
                     |   cryptogen)         |
                     +----------+-----------+
                                |
                       MSP dirs + TLS certs
                                |
                                v
                     +----------------------+
                     |  2. Write            |
                     |  configtx.yaml       |
                     |  - Define all orgs   |
                     |  - Configure Arma    |
                     |  - Set policies      |
                     |  - Set endpoints     |
                     +----------+-----------+
                                |
                                v
                     +----------------------+
                     |  3. Write Arma       |
                     |  shared config       |
                     |  (arma_shared_       |
                     |   config.binpb)      |
                     |  - Party configs     |
                     |  - SmartBFT params   |
                     |  - Batching config   |
                     +----------+-----------+
                                |
                                v
                     +----------------------+
                     |  4. Run configtxgen  |
                     |  -profile MyProfile  |
                     |  -channelID mynet    |
                     |  -outputBlock        |
                     |   genesis.block      |
                     +----------+-----------+
                                |
                                v
                         genesis.block
                    (entire network topology)
                                |
            +-------------------+-------------------+
            |                   |                   |
            v                   v                   v
    +--------------+    +--------------+    +--------------+
    | Orderer      |    | Orderer      |    | Peer / Commi-|
    | Party 0      |    | Party 1      |    | tter nodes   |
    | (Router,     |    | (Router,     |    | boot with    |
    |  Batcher,    |    |  Batcher,    |    | genesis.block|
    |  Consenter,  |    |  Consenter,  |    +--------------+
    |  Assembler)  |    |  Assembler)  |
    | boot with    |    | boot with    |
    | genesis.block|    | genesis.block|
    +--------------+    +--------------+
```

---

## 6. configtx.yaml Section-by-Section Reference

### 6.1 Organizations

The `Organizations` section defines every participant in the network. In
Fabric-X, organizations typically participate in both the ordering service
and the application layer.

```yaml
Organizations:
  - &Org1
    Name: Org1                       # Config group key (alphanumeric, dots, dashes)
    ID: Org1                         # MSP identifier (used in policy rules)
    MSPDir: crypto/Org1/msp          # Path to MSP directory
    SkipAsForeign: false             # Must be false for genesis block generation
    Policies:
      Readers:
        Type: Signature
        Rule: OR('Org1.member')
      Writers:
        Type: Signature
        Rule: OR('Org1.member')
      Admins:
        Type: Signature
        Rule: OR('Org1.admin')
      Endorsement:
        Type: Signature
        Rule: OR('Org1.member')
    OrdererEndpoints:                # Fabric-X: separated endpoints with party IDs
      - id=0,broadcast,orderer-1:7050
      - id=0,deliver,orderer-1:7060
```

**Anatomy of an Organization:**

```
Organization
|
+-- Name                 Config group key. Must be unique across all orgs.
+-- ID                   MSP ID. Referenced in policy rules: OR('ID.member')
+-- MSPDir               Filesystem path to MSP directory (resolved relative
|                        to configtx.yaml location)
+-- MSPType              Provider type. Defaults to "FABRIC" (BCCSP-based).
+-- SkipAsForeign        Must be false for genesis block generation.
+-- AdminPrincipal       Deprecated. Defaults to "Role.ADMIN".
|
+-- Policies             Per-org access control policies
|   +-- Readers          Who can query this org's data
|   +-- Writers          Who can submit transactions for this org
|   +-- Admins           Who can perform admin operations for this org
|   +-- Endorsement      Who can endorse transactions for this org
|
+-- OrdererEndpoints     Fabric-X endpoints with party IDs and API separation
|                        (see Section 8 for full format reference)
|
+-- AnchorPeers          Optional: gossip peer discovery hints
    +-- Host             Anchor peer hostname
    +-- Port             Anchor peer port
```

**MSP Directory Structure:**

The MSPDir must contain the organization's cryptographic material:

```
MSPDir/
+-- cacerts/                 Root CA certificates (trust anchors)
|   +-- ca-cert.pem
+-- admincerts/              Admin identity certificates
|   +-- Admin-cert.pem       (optional if NodeOUs are enabled)
+-- signcerts/               Signing identity certificate
|   +-- cert.pem
+-- keystore/                Private keys (NOT included in genesis block)
|   +-- key.pem
+-- tlscacerts/              TLS root CA certificates
|   +-- tlsca-cert.pem
+-- config.yaml              NodeOU configuration (optional)
```

> **Important:** The `keystore/` directory contains private keys and is
> **never embedded** in the genesis block. Only public certificates from
> `cacerts/`, `admincerts/`, `signcerts/`, and `tlscacerts/` are included.

---

### 6.2 Capabilities

Capabilities are **binary compatibility gates**. They ensure all nodes in the
network support a given feature set before it is activated. A node that
encounters an unrecognized required capability will refuse to process blocks
rather than risk inconsistent state.

```yaml
Capabilities:
  Channel: &ChannelCapabilities       # Both orderers and peers must support
    V3_0: true
  Orderer: &OrdererCapabilities       # Only orderer nodes must support
    V2_0: true
  Application: &ApplicationCapabilities  # Only peer nodes must support
    V2_5: true
```

**Capability enforcement flow:**

```
  Genesis block contains:  Capabilities: { "V3_0": true }

            +------------------+          +------------------+
            | Orderer node     |          | Peer node        |
            | Binary: v3.x     |          | Binary: v3.x     |
            | Recognizes V3_0  |          | Recognizes V3_0  |
            |                  |          |                  |
            | Result: OK,      |          | Result: OK,      |
            |   proceed        |          |   proceed        |
            +------------------+          +------------------+

            +------------------+
            | Peer node        |
            | Binary: v2.x     |
            | Does NOT know    |
            | V3_0             |
            |                  |
            | Result: HALT     |
            | (requires        |
            |  upgrade)        |
            +------------------+
```

**Capabilities required for Fabric-X:**

```
+---------------------+---------+------------------------------------------+
| Capability          | Level   | What it enables                          |
+---------------------+---------+------------------------------------------+
| V3_0                | Channel | Required for Fabric-X.                   |
|                     |         | - Per-org orderer endpoints (no global)  |
|                     |         | - BFT/Arma orderer type support          |
|                     |         | - Removal of system channel              |
+---------------------+---------+------------------------------------------+
| V2_0                | Orderer | Raft consensus & orderer capability      |
|                     |         | checks                                   |
+---------------------+---------+------------------------------------------+
| V2_5                | App     | Private data purge, advanced chaincode   |
|                     |         | lifecycle                                |
+---------------------+---------+------------------------------------------+
```

> **Note:** `V3_0` is mandatory for Fabric-X. It enforces per-org orderer
> endpoints and disallows the deprecated global `Orderer.Addresses`.

---

### 6.3 Application

The `Application` section configures the peer and committer side of the
network. It defines ACLs, policies (including the lifecycle endorsement
policy used for namespace creation), and capabilities.

```yaml
Application: &ApplicationDefaults
  ACLs:
    # Chaincode lifecycle
    _lifecycle/CheckCommitReadiness:       /Channel/Application/Writers
    _lifecycle/CommitChaincodeDefinition:   /Channel/Application/Writers
    _lifecycle/QueryChaincodeDefinition:    /Channel/Application/Writers
    _lifecycle/QueryChaincodeDefinitions:   /Channel/Application/Writers
    # Legacy system chaincodes
    lscc/ChaincodeExists:                  /Channel/Application/Readers
    lscc/GetDeploymentSpec:                /Channel/Application/Readers
    lscc/GetChaincodeData:                 /Channel/Application/Readers
    lscc/GetInstantiatedChaincodes:        /Channel/Application/Readers
    # Query system chaincode
    qscc/GetChainInfo:                     /Channel/Application/Readers
    qscc/GetBlockByNumber:                 /Channel/Application/Readers
    qscc/GetBlockByHash:                   /Channel/Application/Readers
    qscc/GetTransactionByID:               /Channel/Application/Readers
    qscc/GetBlockByTxID:                   /Channel/Application/Readers
    # Config system chaincode
    cscc/GetConfigBlock:                   /Channel/Application/Readers
    cscc/GetChannelConfig:                 /Channel/Application/Readers
    # Peer operations
    peer/Propose:                          /Channel/Application/Writers
    peer/ChaincodeToChaincode:             /Channel/Application/Writers
    # Events
    event/Block:                           /Channel/Application/Readers
    event/FilteredBlock:                   /Channel/Application/Readers

  Policies:
    LifecycleEndorsement:
      Type: ImplicitMeta
      Rule: MAJORITY Endorsement
    Endorsement:
      Type: ImplicitMeta
      Rule: MAJORITY Endorsement
    Readers:
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:
      Type: ImplicitMeta
      Rule: MAJORITY Admins

  Capabilities:
    <<: *ApplicationCapabilities
```

**Namespace Creation via Lifecycle Endorsement Policy:**

Fabric-X uses application namespaces to isolate different workloads. New
namespaces are created through the **lifecycle endorsement policy**
(`LifecycleEndorsement`), which requires a `MAJORITY` of organization
endorsements by default.

```
+-----------------------------------------------------------------------+
|  Fabric-X Namespace Architecture:                                     |
|                                                                       |
|    _meta    -- Meta-namespace for system configuration transactions   |
|    _config  -- Config namespace for namespace policy management       |
|    app_ns   -- Application-defined namespaces (user-created)          |
|                                                                       |
|  Creating a new namespace requires satisfying the LifecycleEndorse-   |
|  ment policy, which by default is:                                    |
|                                                                       |
|    ImplicitMeta "MAJORITY Endorsement"                                |
|                                                                       |
|  This means a majority of the network's organizations must endorse    |
|  the namespace creation transaction.                                  |
|                                                                       |
|    Namespace creation request                                         |
|         |                                                             |
|         v                                                             |
|    Evaluate LifecycleEndorsement policy                               |
|         |                                                             |
|         +---> Org1/Endorsement: OR('Org1.member')  --> signed? YES    |
|         +---> Org2/Endorsement: OR('Org2.member')  --> signed? YES    |
|         |                                                             |
|         v                                                             |
|    MAJORITY satisfied (2/2) --> Namespace created                     |
+-----------------------------------------------------------------------+
```

Each namespace has its own policy (`NamespacePolicy`) that governs
transaction validation within that namespace. A `NamespacePolicy` can use
either:
- A **ThresholdRule** with a signature scheme and public key, or
- An **MSP rule** (raw MSP policy bytes)

```
NamespacePolicy
|
+-- ThresholdRule           Signature-based validation
|   +-- scheme              Signature scheme (e.g., "ECDSA")
|   +-- public_key          Public key for verification
|
+-- MspRule                 MSP-based validation
    +-- <raw MSP policy>    Validates against org MSP policies
```

**ACL Resource Mapping:**

ACLs map operations on system chaincodes and events to policy paths. The
value is a policy path that references a named policy elsewhere in the
configuration tree:

```
  ACL resource                          References this policy
  ==========================            ===================================
  peer/Propose                   --->   /Channel/Application/Writers
                                              |
                                              v
                                        ImplicitMeta "ANY Writers"
                                              |
                                   +----------+-----------+
                                   |                      |
                                   v                      v
                              Org1/Writers           Org2/Writers
                              Signature:             Signature:
                              OR('Org1.member')      OR('Org2.member')

  "ANY" means at least one org's Writers policy must be satisfied.
```

---

### 6.4 Orderer

The `Orderer` section configures the ordering service. In Fabric-X, the
primary consensus type is **Arma**, which uses a decomposed architecture
of specialized nodes.

```yaml
Orderer: &OrdererDefaults
  OrdererType: arma              # "arma" for Fabric-X (also: "etcdraft", "BFT")

  BatchTimeout: 2s               # Max time before cutting a block

  BatchSize:
    MaxMessageCount: 500          # Max transactions per block
    AbsoluteMaxBytes: 10 MB       # Hard cap on block data size
    PreferredMaxBytes: 2 MB       # Soft target for block data size

  MaxChannels: 0                  # Not applicable in single-network model

  ConsenterMapping:               # Required for Arma and BFT
    - ID: 1
      Host: bft0.example.com
      Port: 7050
      MSPID: Org1
      Identity: crypto/Org1/msp/admincerts/Admin@Org1-cert.pem
      ClientTLSCert: crypto/Org1/tls/client.pem
      ServerTLSCert: crypto/Org1/tls/server.pem

  Arma:                           # Arma-specific configuration
    Path: arma_shared_config.binpb  # Binary protobuf with party topology

  Policies:
    Readers:
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:
      Type: ImplicitMeta
      Rule: MAJORITY Admins
    BlockValidation:              # Required: validates orderer block signatures
      Type: ImplicitMeta
      Rule: MAJORITY Writers      # MAJORITY for BFT-based ordering

  Capabilities:
    <<: *OrdererCapabilities
```

**Block Cutting Logic:**

The ordering service cuts a new block when **any** of these conditions is met:

```
  Incoming transactions accumulate in the pending batch...

      +--------------------+
      | BatchTimeout >= 2s |---YES---> CUT BLOCK (even if only 1 tx)
      +--------------------+

      +-------------------------+
      | tx count >= 500         |---YES---> CUT BLOCK
      | (MaxMessageCount)       |
      +-------------------------+

      +-------------------------+
      | batch bytes >= 10 MB    |---YES---> CUT BLOCK
      | (AbsoluteMaxBytes)      |           (any single tx > 10 MB is REJECTED)
      +-------------------------+

      +-------------------------+
      | batch bytes >= 2 MB     |---YES---> CUT BLOCK
      | (PreferredMaxBytes)     |           (new tx starts next batch)
      +-------------------------+
```

**Batch size parameters on a number line:**

```
  0                     2 MB                          10 MB
  |------+--------------|-+----------------------------|--> bytes
         |              | |                            |
         | Normal txs   | | Single large tx gets       | REJECTED
         | accumulate   | | its own 1-tx block         | (too big)
         |              | |                            |
         |<- Preferred ->|                             |
         |              |<-------- Absolute ---------->|
```

**ConsenterMapping fields:**

Each entry in `ConsenterMapping` identifies a consenter (ordering node) in the
network:

```
ConsenterMapping entry
|
+-- ID               Unique numeric identifier for this consenter
+-- Host              Hostname for consensus communication
+-- Port              Port for consensus communication
+-- MSPID             MSP ID of the organization this consenter belongs to
+-- Identity          Path to identity certificate (signing cert)
+-- ClientTLSCert     Path to client-side TLS certificate
+-- ServerTLSCert     Path to server-side TLS certificate
```

> **Note:** For Arma and BFT, configtxgen reads the certificate files from
> disk and embeds their contents (not paths) into the genesis block.

---

### 6.5 Channel

The `Channel` section defines top-level policies and capabilities for the
entire network. In Fabric-X, think of "Channel" as "Network" -- it is the
root configuration group.

```yaml
Channel: &ChannelDefaults
  Policies:
    Readers:                    # Who may invoke the Deliver API
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:                    # Who may invoke the Broadcast API
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:                     # Who may modify the network configuration
      Type: ImplicitMeta
      Rule: MAJORITY Admins
  Capabilities:
    <<: *ChannelCapabilities    # V3_0 required for Fabric-X
```

---

### 6.6 Profiles

Profiles are the **entry point** for `configtxgen`. A profile composes the
templates from the top-level sections into a complete network definition.

A Fabric-X genesis block profile must include:
- An `Orderer` section (with organizations, ConsenterMapping, and Arma config)
- An `Application` section (with organizations and lifecycle endorsement policy)
- Channel-level policies and capabilities (V3_0)

**The canonical Fabric-X profile (from sampleconfig/configtx.yaml):**

```yaml
Profiles:
  SampleFabricX:
    <<: *ChannelDefaults
    Orderer:
      <<: *OrdererDefaults
      OrdererType: arma
      Policies:
        <<: *DefaultOrdererPolicies
        BlockValidation:
          Type: ImplicitMeta
          Rule: MAJORITY Writers        # BFT requires MAJORITY
      Organizations:
        - <<: *SampleOrg
          OrdererEndpoints:
            - id=0,broadcast,orderer-1:7050
            - id=0,deliver,orderer-1:7060
            - id: 1
              api: [broadcast, deliver]
              host: orderer-2
              port: 7050
    Application:
      <<: *ApplicationDefaults
      Organizations:
        - <<: *SampleOrg
```

**Multi-org Fabric-X profile:**

```yaml
  TwoOrgsSampleFabricX:
    <<: *ChannelDefaults
    Orderer:
      <<: *FabricXOrdererDefaults
      Organizations:
        - <<: *Org1
          OrdererEndpoints:
            - id=0,broadcast,localhost:7050
            - id=0,deliver,localhost:7060
        - <<: *Org2
          OrdererEndpoints:
            - id=1,broadcast,localhost:7051
            - id=1,deliver,localhost:7061
    Application:
      <<: *FabricXApplicationDefaults
      Organizations:
        - <<: *Org1
        - <<: *Org2
```

**How profiles compose the final configuration:**

```
    configtx.yaml top-level sections         Profile (composition)
    ====================================     ==========================

    +--------------------+                   Profiles:
    | Organizations:     |    reference         TwoOrgsSampleFabricX:
    |   - &Org1 {...}    |<------------------     <<: *ChannelDefaults
    |   - &Org2 {...}    |                        Orderer:
    +--------------------+                          <<: *OrdererDefaults
    | Orderer:           |    merge                 OrdererType: arma
    |   &OrdererDefaults |<------------------       Organizations:
    |   BatchTimeout: 2s |                            - *Org1
    +--------------------+                            - *Org2
    | Application:       |    merge               Application:
    |   &AppDefaults     |<------------------       <<: *AppDefaults
    |   ACLs: {...}      |                          Organizations:
    +--------------------+                            - *Org1
    | Channel:           |    merge                   - *Org2
    |   &ChannelDefaults |<------------------
    +--------------------+

    configtxgen -profile TwoOrgsSampleFabricX:
    1. Resolves all YAML anchors and merges
    2. Produces one fully-resolved Profile struct
    3. Converts it to a protobuf ConfigGroup hierarchy
    4. Wraps it in a genesis Block
```

---

## 7. Arma Consensus: The Fabric-X Ordering Architecture

Arma is the consensus protocol purpose-built for Fabric-X. Unlike Raft (which
uses monolithic orderer nodes), Arma decomposes the ordering service into
**four specialized node roles** per party.

### 7.1 Arma Node Roles

```
+-----------------------------------------------------------------------+
|                    ARMA PARTY ARCHITECTURE                            |
|                                                                       |
|   Each "party" (organization) runs these node roles:                  |
|                                                                       |
|   +-------------------+     +-----------------+                       |
|   |     Router        |     |    Batcher(s)   |                       |
|   | - Receives client |---->| - Accumulates   |                       |
|   |   transactions    |     |   transactions  |                       |
|   |   (broadcast API) |     |   into batches  |                       |
|   | - Delivers blocks |     | - One per shard |                       |
|   |   to clients      |     | - Signs batch   |                       |
|   |   (deliver API)   |     |   attestations  |                       |
|   +-------------------+     +--------+--------+                       |
|                                      |                                |
|                               batches|                                |
|                                      v                                |
|                             +------------------+                      |
|                             |    Consenter     |                      |
|                             | - Runs SmartBFT  |                      |
|                             |   consensus      |                      |
|                             | - Agrees on      |                      |
|                             |   block ordering |                      |
|                             | - Signs blocks   |                      |
|                             +--------+---------+                      |
|                                      |                                |
|                               agreed |order                           |
|                                      v                                |
|                             +------------------+                      |
|                             |   Assembler      |                      |
|                             | - Assembles      |                      |
|                             |   final blocks   |                      |
|                             | - Writes to      |                      |
|                             |   the ledger     |                      |
|                             +------------------+                      |
+-----------------------------------------------------------------------+
```

### 7.2 Arma SharedConfig (arma_shared_config.binpb)

The Arma consensus metadata is stored in an external binary protobuf file
referenced by `Orderer.Arma.Path`. This file contains the complete party
topology:

```
SharedConfig (binary protobuf)
|
+-- PartiesConfig[]              One entry per party in the network
|   +-- PartyID                  Unique party identifier (uint32, > 0)
|   +-- CACerts[]                CA certificates for this party
|   +-- TLSCACerts[]             TLS CA certificates for this party
|   +-- RouterConfig             Router node configuration
|   |   +-- host                 Router hostname
|   |   +-- port                 Router port
|   |   +-- tls_cert             Router TLS certificate
|   +-- BatchersConfig[]         One or more batcher nodes (sharded)
|   |   +-- shardID              Shard this batcher handles
|   |   +-- host                 Batcher hostname
|   |   +-- port                 Batcher port
|   |   +-- sign_cert            Batcher signing certificate
|   |   +-- tls_cert             Batcher TLS certificate
|   +-- ConsenterConfig          Consenter (BFT consensus) node
|   |   +-- host                 Consenter hostname
|   |   +-- port                 Consenter port
|   |   +-- sign_cert            Consenter signing certificate
|   |   +-- tls_cert             Consenter TLS certificate
|   +-- AssemblerConfig          Assembler (block assembly) node
|       +-- host                 Assembler hostname
|       +-- port                 Assembler port
|       +-- tls_cert             Assembler TLS certificate
|
+-- ConsensusConfig              SmartBFT consensus parameters
|   +-- SmartBFTConfig
|       +-- RequestBatchMaxCount
|       +-- RequestBatchMaxBytes
|       +-- RequestBatchMaxInterval
|       +-- RequestForwardTimeout
|       +-- RequestComplainTimeout
|       +-- ViewChangeTimeout
|       +-- LeaderHeartbeatTimeout
|       +-- LeaderHeartbeatCount
|       +-- CollectTimeout
|       +-- ... (and more BFT tuning parameters)
|
+-- BatchingConfig               Batching parameters
    +-- BatchTimeouts
    |   +-- BatchCreationTimeout       Max time before creating a batch
    |   +-- FirstStrikeThreshold       Time before forwarding to primary batcher
    |   +-- SecondStrikeThreshold      Time before suspecting primary censorship
    |   +-- AutoRemoveTimeout          Time before removing stale requests
    +-- BatchSize
    |   +-- MaxMessageCount            Max transactions per batch
    |   +-- AbsoluteMaxBytes           Hard cap on batch size
    |   +-- PreferredMaxBytes          Soft target (not currently used)
    +-- RequestMaxBytes                Max size of a single request
```

### 7.3 Arma Transaction Flow

```
  Client                 Party 0                            Party 1
    |                      |                                  |
    | 1. Submit tx         |                                  |
    |----(broadcast)------>| Router                           |
    |                      |   |                              |
    |                      |   | 2. Forward to batcher        |
    |                      |   v                              |
    |                      | Batcher                          |
    |                      |   |                              |
    |                      |   | 3. Accumulate into batch     |
    |                      |   |    (until timeout/size)      |
    |                      |   |                              |
    |                      |   | 4. Send batch to consenter   |
    |                      |   v                              |
    |                      | Consenter  <--- SmartBFT --->  Consenter
    |                      |   |          consensus           |
    |                      |   |          protocol            |
    |                      |   |                              |
    |                      |   | 5. Agree on block order      |
    |                      |   v                              |
    |                      | Assembler                     Assembler
    |                      |   |                              |
    |                      |   | 6. Assemble & sign block     |
    |                      |   |                              |
    |                      |   | 7. Write to ledger           |
    |                      |   v                              |
    | 8. Deliver block     | Router                           |
    |<---(deliver)---------|                                  |
```

### 7.4 Arma vs Other Consensus Types

```
+-------------------------------------------------------------------+
|             Raft (etcdraft)   |  BFT (SmartBFT)  |  Arma          |
|-------------------------------|-------------------|----------------|
| Fault model:                  | Fault model:      | Fault model:   |
|   Crash (CFT)                 |   Byzantine (BFT) |   Byzantine    |
|                               |                   |                |
| Formula: 2F+1                 | Formula: 3F+1     | Formula: 3F+1  |
|   1 failure -> 3 nodes        |   1 failure -> 4  |   1 failure -> |
|   2 failures -> 5 nodes       |   2 failures -> 7 |   4 parties    |
|                               |                   |                |
| Architecture:                 | Architecture:     | Architecture:  |
|   Monolithic orderer          |   Monolithic      |   Decomposed:  |
|   nodes with Raft             |   orderer nodes   |   Router +     |
|   leader election             |   with SmartBFT   |   Batcher +    |
|                               |                   |   Consenter +  |
|                               |                   |   Assembler    |
|                               |                   |                |
| Endpoints:                    | Endpoints:        | Endpoints:     |
|   Combined broadcast          |   Combined        |   Separated    |
|   & deliver per node          |                   |   broadcast &  |
|                               |                   |   deliver per  |
|                               |                   |   party        |
|                               |                   |                |
| Config in genesis:            | Config in genesis: | Config:       |
|   EtcdRaft.Consenters         |   ConsenterMapping |  Consenter-   |
|   EtcdRaft.Options            |   SmartBFT options |  Mapping +    |
|                               |                   |  external .bin |
+-------------------------------------------------------------------+
```

---

## 8. Orderer Endpoints with Party IDs and Separated APIs

Fabric-X introduces a richer orderer endpoint format compared to legacy Fabric.
Each endpoint can specify:
- A **party ID** identifying which ordering party it belongs to
- A specific **API** it supports (`broadcast`, `deliver`, or both)

This allows Fabric-X to expose different network addresses for submitting
transactions (broadcast) and retrieving blocks (deliver).

### 8.1 Endpoint Fields

```
OrdererEndpoint
|
+-- host       (required)  Hostname or IP address
+-- port       (required)  Port number
+-- id         (optional)  Party ID (uint32). Default: unspecified.
+-- api        (optional)  Supported APIs: ["broadcast"] and/or ["deliver"]
|                          Default: both broadcast and deliver.
+-- msp-id     (optional)  MSP ID of the organization
```

### 8.2 Encoding Formats

Orderer endpoints can be encoded in **three formats**. configtxgen tries each
in order until one succeeds:

**Format 1: YAML (inline or block)**

```yaml
OrdererEndpoints:
  - host: orderer-1
    port: 7050
    id: 0
    api:
      - broadcast
      - deliver
```

**Format 2: JSON (inline)**

```yaml
OrdererEndpoints:
  - {host: "orderer-1", port: 7050, id: 0, api: ["broadcast", "deliver"]}
```

**Format 3: Compact string**

```yaml
OrdererEndpoints:
  - id=0,broadcast,deliver,orderer-1:7050
  - id=0,broadcast,host=orderer-1,port=7050    # equivalent
  - orderer-1:7050                              # simple (no id, all APIs)
```

### 8.3 Common Patterns

**Separated broadcast and deliver per party:**

```yaml
# Party 0 has separate endpoints for broadcast and deliver
OrdererEndpoints:
  - id=0,broadcast,orderer-1:7050      # clients submit txs here
  - id=0,deliver,orderer-1:7060        # clients fetch blocks here
```

**Multiple parties, each with separated endpoints:**

```yaml
# Org1 runs Party 0
- <<: *Org1
  OrdererEndpoints:
    - id=0,broadcast,localhost:7050
    - id=0,deliver,localhost:7060

# Org2 runs Party 1
- <<: *Org2
  OrdererEndpoints:
    - id=1,broadcast,localhost:7051
    - id=1,deliver,localhost:7061
```

**Visual: How clients use separated endpoints:**

```
  Client Application
       |
       |  Submit transaction (broadcast)
       |-------------------------------------> Party 0 Router :7050
       |                                        (broadcast endpoint)
       |
       |  Fetch blocks (deliver)
       |-------------------------------------> Party 0 Router :7060
       |                                        (deliver endpoint)
       |
       |  Can also connect to Party 1:
       |-------------------------------------> Party 1 Router :7051
       |-------------------------------------> Party 1 Router :7061
```

---

## 9. Policies Deep Dive

Policies govern who can perform actions on the network. There are two types:

### 9.1 Signature Policies

Explicitly specify which MSP principals must sign a request:

```
Type: Signature

Rule: OR('Org1.member')                 Any member of Org1
Rule: OR('Org1.admin')                  Any admin of Org1
Rule: AND('Org1.member', 'Org2.member') One member from EACH org
Rule: OR('Org1.member', 'Org2.member')  One member from EITHER org
```

Used at the **organization level** for fine-grained control.

### 9.2 ImplicitMeta Policies

Aggregate sub-policies from child config groups:

```
Type: ImplicitMeta

Rule: ANY <SubPolicy>        At least one child satisfies <SubPolicy>
Rule: ALL <SubPolicy>        Every child satisfies <SubPolicy>
Rule: MAJORITY <SubPolicy>   > 50% of children satisfy <SubPolicy>
```

Used at **channel, orderer, and application levels** to compose org-level
policies.

### 9.3 How ImplicitMeta Aggregation Works

```
  Example: Network has Org1 and Org2

  Channel/Writers = ImplicitMeta "ANY Writers"
       |
       +-----> Orderer/Writers = ImplicitMeta "ANY Writers"
       |            |
       |            +-> Org1/Writers = Signature OR('Org1.member')
       |            +-> Org2/Writers = Signature OR('Org2.member')
       |            ANY = at least 1 org's Writers must be satisfied
       |
       +-----> Application/Writers = ImplicitMeta "ANY Writers"
                    |
                    +-> Org1/Writers = Signature OR('Org1.member')
                    +-> Org2/Writers = Signature OR('Org2.member')
                    ANY = at least 1 org's Writers must be satisfied

  Result: A member of Org1 OR Org2 can invoke the Broadcast API.


  Channel/Admins = ImplicitMeta "MAJORITY Admins"
       |
       +-----> Orderer/Admins = ImplicitMeta "MAJORITY Admins"
       |            |
       |            +-> Org1/Admins = Signature OR('Org1.admin')
       |            +-> Org2/Admins = Signature OR('Org2.admin')
       |            MAJORITY of 2 = BOTH must be satisfied
       |
       +-----> Application/Admins = ImplicitMeta "MAJORITY Admins"
                    |
                    +-> Org1/Admins = Signature OR('Org1.admin')
                    +-> Org2/Admins = Signature OR('Org2.admin')
                    MAJORITY of 2 = BOTH must be satisfied

  Result: Admins from BOTH orgs must sign to modify network config.
```

### 9.4 Policy Paths

```
/Channel/Readers                        Network-level read access (Deliver API)
/Channel/Writers                        Network-level write access (Broadcast API)
/Channel/Admins                         Network-level admin (config updates)
/Channel/Orderer/Readers                Orderer read access
/Channel/Orderer/Writers                Orderer write access
/Channel/Orderer/Admins                 Orderer admin
/Channel/Orderer/BlockValidation        Block signature verification
/Channel/Application/Readers            Application read access
/Channel/Application/Writers            Application write access
/Channel/Application/Admins             Application admin
/Channel/Application/LifecycleEndorsement   Chaincode lifecycle endorsement
/Channel/Application/Endorsement        Chaincode execution endorsement
/Channel/Orderer/<OrgName>/Readers      Per-org orderer policies
/Channel/Application/<OrgName>/Readers  Per-org application policies
```

---

## 10. How configtxgen Generates the Genesis Block

### 10.1 High-Level Pipeline

```
+------------------+
| configtx.yaml    |
+--------+---------+
         |
         | 1. Viper YAML parser
         v
+------------------+       +---------------------+
| TopLevel struct  |------>| configCache          |
| {                |       | (caches parsed       |
|   Profiles       |       |  config by filepath) |
|   Organizations  |       +---------------------+
|   Orderer        |
|   Application    |
|   Capabilities   |
| }                |
+--------+---------+
         |
         | 2. Select profile by name
         v
+------------------+
| Profile struct   |
| {                |
|   Orderer        |
|   Application    |
|   Policies       |
|   Capabilities   |
| }                |
+--------+---------+
         |
         | 3. CompleteInitialization()
         |    - Fill defaults for unset fields
         |    - Resolve all paths to absolute
         |    - Validate consensus-specific config
         |    - Load MSP certificates from disk
         |    - Load TLS certificates for consenters
         v
+------------------+
| Initialized      |
| Profile          |
+--------+---------+
         |
         | 4. NewChannelGroup(profile)
         |    Build protobuf ConfigGroup hierarchy
         v
+---------------------+
| ConfigGroup (root)  |
| +-- Orderer/        |
| |   +-- Org1/       |
| |   +-- Org2/       |
| +-- Application/    |
| |   +-- Org1/       |
| |   +-- Org2/       |
| +-- Policies        |
| +-- Values          |
+--------+------------+
         |
         | 5. genesis.Factory.Block(channelID)
         |    Wrap ConfigGroup in Block protobuf
         v
+------------------+
| Block #0         |
| - Header         |
| - Data           |
| - Metadata       |
+--------+---------+
         |
         | 6. proto.Marshal + write to disk
         v
+------------------+
| genesis.block    |
| (binary protobuf |
|  file, perm 0640)|
+------------------+
```

### 10.2 Step 3 Detail: CompleteInitialization

```
Profile.CompleteInitialization(configDir)
|
+---> For each org (in Orderer and Application sections):
|       - MSPType defaults to "FABRIC" if empty
|       - AdminPrincipal defaults to "Role.ADMIN" if empty
|       - MSPDir resolved from relative to absolute path
|
+---> Orderer defaults filled in:
|
|       +------------------------------+-------------------+
|       | Field                        | Default           |
|       +------------------------------+-------------------+
|       | OrdererType                  | "solo" (override  |
|       |                              |  to "arma")       |
|       | BatchTimeout                 | 2 seconds         |
|       | BatchSize.MaxMessageCount    | 500               |
|       | BatchSize.AbsoluteMaxBytes   | 10 MB             |
|       | BatchSize.PreferredMaxBytes  | 2 MB              |
|       +------------------------------+-------------------+
|
+---> Arma-specific validation:
        - Arma.Path resolved to absolute path
        - ConsenterMapping must be non-empty
        - Each consenter validated:
            +-- Host must be set
            +-- Port must be set
            +-- MSPID must be set
            +-- Identity cert path must be set
            +-- ClientTLSCert path must be set
            +-- ServerTLSCert path must be set
        - All cert/identity paths resolved to absolute
```

### 10.3 Step 4 Detail: Building the ConfigGroup Hierarchy

```
NewChannelGroup(profile)
|
+-- Add channel-level policies (Readers, Writers, Admins)
+-- Add HashingAlgorithm value ("SHA256")
+-- Add BlockDataHashingStructure value (width: MaxUint32)
+-- Add Capabilities value ({"V3_0": {}})
|
+-- NewOrdererGroup(profile.Orderer)
|   |
|   +-- Validate: No global Addresses with V3_0
|   +-- Add orderer policies (Readers, Writers, Admins, BlockValidation)
|   +-- Add BatchSize value
|   +-- Add BatchTimeout value
|   +-- Add ChannelRestrictions value
|   +-- Add Capabilities value
|   |
|   +-- [Arma-specific]:
|   |   +-- Read consenter mapping -> []*cb.Consenter protos
|   |   |   (loads Identity, ClientTLSCert, ServerTLSCert from disk)
|   |   +-- Add Orderers value (consenter list embedded in genesis)
|   |   +-- Read Arma.Path -> consensus metadata bytes
|   |   +-- Add ConsensusType value {type: "arma", metadata: <bytes>}
|   |
|   +-- For each orderer org:
|       +-- NewOrdererOrgGroup(org)
|           +-- Load MSP config from MSPDir
|           +-- Add org policies (Readers, Writers, Admins)
|           +-- Add MSP value (embedded certificates)
|           +-- Add Endpoints value (serialized OrdererEndpoints)
|
+-- NewApplicationGroup(profile.Application)
|   |
|   +-- Add application policies (including LifecycleEndorsement)
|   +-- Add ACLs value
|   +-- Add Capabilities value
|   |
|   +-- For each application org:
|       +-- NewApplicationOrgGroup(org)
|           +-- Load MSP config from MSPDir
|           +-- Add org policies (Readers, Writers, Admins, Endorsement)
|           +-- Add MSP value
|           +-- Add AnchorPeers value (if any)
|
+-- Set ModPolicy = "Admins"
+-- Return root ConfigGroup
```

### 10.4 Step 5 Detail: Creating the Genesis Block

```
genesis.Factory.Block(channelID)
|
+-- Create ChannelHeader
|     Type: HeaderType_CONFIG
|     Version: 1
|     ChannelId: channelID         (identifies the network)
|     Epoch: 0
|     TxId: <generated from nonce>
|
+-- Create SignatureHeader
|     Creator: nil                 (genesis block has no signer)
|     Nonce: <random bytes>
|
+-- Create Payload
|     Header: {ChannelHeader, SignatureHeader}
|     Data: Marshal(ConfigEnvelope{
|               Config: {
|                   ChannelGroup: <the ConfigGroup hierarchy>
|               }
|           })
|
+-- Create Envelope
|     Payload: Marshal(payload)
|     Signature: nil
|
+-- Create Block #0
|     Header:
|       Number: 0                  (first block ever)
|       PreviousHash: nil          (no previous block)
|       DataHash: SHA256(Data)
|     Data:
|       Data: [ Marshal(envelope) ]
|     Metadata:
|       [LAST_CONFIG]:  LastConfig{Index: 0}
|       [SIGNATURES]:   OrdererBlockMetadata{LastConfig{Index: 0}}
|
+-- Return block
```

---

## 11. Genesis Block Internal Structure

The genesis block has the following nested protobuf structure. This is what
you see when you run `configtxgen -inspectBlock genesis.block`:

```
Block
+-- Header
|   +-- Number: 0                              First block
|   +-- PreviousHash: nil                      No predecessor
|   +-- DataHash: SHA256(Data)
|
+-- Data
|   +-- Data[0]: Envelope (serialized)
|       +-- Payload (serialized)
|       |   +-- Header
|       |   |   +-- ChannelHeader
|       |   |   |   +-- Type: CONFIG (1)
|       |   |   |   +-- Version: 1
|       |   |   |   +-- ChannelId: "mynetwork"
|       |   |   |   +-- Epoch: 0
|       |   |   |   +-- TxId: "<generated>"
|       |   |   +-- SignatureHeader
|       |   |       +-- Creator: nil
|       |   |       +-- Nonce: <random>
|       |   +-- Data: ConfigEnvelope (serialized)
|       |       +-- Config
|       |           +-- Sequence: 0
|       |           +-- ChannelGroup --------+
|       +-- Signature: nil                   |
|                                            |
+-- Metadata                                 |
    +-- [LAST_CONFIG]:  {Index: 0}           |
    +-- [SIGNATURES]:   {LastConfig: {0}}    |
                                             |
    +----------------------------------------+
    |
    v
  ConfigGroup (ROOT -- the entire network configuration)
  +-- ModPolicy: "Admins"
  +-- Policies:
  |   +-- Readers:  ImplicitMeta "ANY Readers"
  |   +-- Writers:  ImplicitMeta "ANY Writers"
  |   +-- Admins:   ImplicitMeta "MAJORITY Admins"
  +-- Values:
  |   +-- HashingAlgorithm:           "SHA256"
  |   +-- BlockDataHashingStructure:  width = 4294967295
  |   +-- Capabilities:               {"V3_0": {}}
  +-- Groups:
      +-- Orderer/    (see Section 12)
      +-- Application/ (see Section 12)
```

---

## 12. The Network Configuration Hierarchy

The complete configuration tree embedded in the genesis block:

```
Channel/                                           (root ConfigGroup)
|
+-- Policies/
|   +-- Readers             ImplicitMeta "ANY Readers"
|   +-- Writers             ImplicitMeta "ANY Writers"
|   +-- Admins              ImplicitMeta "MAJORITY Admins"
|
+-- Values/
|   +-- HashingAlgorithm             "SHA256"
|   +-- BlockDataHashingStructure    width: 4294967295 (MaxUint32)
|   +-- Capabilities                 { "V3_0": {} }
|
+-- Groups/
    |
    +-- Orderer/
    |   +-- Policies/
    |   |   +-- Readers              ImplicitMeta "ANY Readers"
    |   |   +-- Writers              ImplicitMeta "ANY Writers"
    |   |   +-- Admins               ImplicitMeta "MAJORITY Admins"
    |   |   +-- BlockValidation      ImplicitMeta "MAJORITY Writers"
    |   |
    |   +-- Values/
    |   |   +-- ConsensusType        { type: "arma",
    |   |   |                          metadata: <SharedConfig bytes> }
    |   |   +-- BatchSize            { max_message_count: 500,
    |   |   |                          absolute_max_bytes: 10485760,
    |   |   |                          preferred_max_bytes: 2097152 }
    |   |   +-- BatchTimeout         { timeout: "2s" }
    |   |   +-- ChannelRestrictions  { max_count: 0 }
    |   |   +-- Capabilities         { "V2_0": {} }
    |   |   +-- Orderers             [ Consenter protos from
    |   |                              ConsenterMapping ]
    |   |
    |   +-- Groups/
    |       +-- Org1/
    |       |   +-- Policies/
    |       |   |   +-- Readers       Signature OR('Org1.member')
    |       |   |   +-- Writers       Signature OR('Org1.member')
    |       |   |   +-- Admins        Signature OR('Org1.admin')
    |       |   +-- Values/
    |       |       +-- MSP           { embedded CA certs, admin certs, etc. }
    |       |       +-- Endpoints     ["id=0,broadcast,orderer-1:7050",
    |       |                          "id=0,deliver,orderer-1:7060"]
    |       +-- Org2/
    |           +-- (same structure)
    |           +-- Endpoints         ["id=1,broadcast,orderer-2:7050",
    |                                  "id=1,deliver,orderer-2:7060"]
    |
    +-- Application/
        +-- Policies/
        |   +-- Readers              ImplicitMeta "ANY Readers"
        |   +-- Writers              ImplicitMeta "ANY Writers"
        |   +-- Admins               ImplicitMeta "MAJORITY Admins"
        |   +-- LifecycleEndorsement ImplicitMeta "MAJORITY Endorsement"
        |   +-- Endorsement          ImplicitMeta "MAJORITY Endorsement"
        |
        +-- Values/
        |   +-- ACLs                 { resource -> policy path mapping }
        |   +-- Capabilities         { "V2_5": {} }
        |
        +-- Groups/
            +-- Org1/
            |   +-- Policies/
            |   |   +-- Readers       Signature OR('Org1.member')
            |   |   +-- Writers       Signature OR('Org1.member')
            |   |   +-- Admins        Signature OR('Org1.admin')
            |   |   +-- Endorsement   Signature OR('Org1.member')
            |   +-- Values/
            |       +-- MSP           { embedded CA certs, admin certs, etc. }
            +-- Org2/
                +-- (same structure)
```

---

## 13. configtxgen Command Reference

### 13.1 Generate a Genesis Block (primary use case)

```bash
configtxgen \
  -profile SampleFabricX \
  -channelID mynetwork \
  -outputBlock genesis.block \
  -configPath /path/to/config/
```

### 13.2 Inspect a Genesis Block

```bash
configtxgen -inspectBlock genesis.block
```

Outputs the full block structure as JSON to stdout. Useful for verifying the
genesis block content before distributing it to nodes.

### 13.3 Print an Organization Definition

```bash
configtxgen -printOrg Org1 -configPath /path/to/config/
```

Outputs the organization's ConfigGroup as JSON. Useful when you need to
add a new organization to an existing network via a config update.

### 13.4 All Flags

```
+--------------------------+----------------------------------------------------+
| Flag                     | Description                                        |
+--------------------------+----------------------------------------------------+
| -profile <name>          | Profile name from configtx.yaml Profiles section   |
| -channelID <id>          | Network/channel identifier for the genesis block   |
| -outputBlock <path>      | Write genesis block to this file path              |
| -configPath <dir>        | Directory containing configtx.yaml                 |
| -inspectBlock <path>     | Decode and print a block as JSON to stdout         |
| -printOrg <orgName>      | Print an org's ConfigGroup as JSON                 |
| -version                 | Print version info and exit                        |
+--------------------------+----------------------------------------------------+

Deprecated flags (inherited from legacy Fabric, not used in Fabric-X):
  -outputCreateChannelTx     Channel creation transactions (no multi-channel)
  -inspectChannelCreateTx    Inspect channel creation tx
  -channelCreateTxBaseProfile  Base profile for channel creation
  -asOrg                     Filter write set to a specific org
```

### 13.5 Environment Variable

```
FABRIC_CFG_PATH     Path to directory containing configtx.yaml.
                    Used when -configPath is not provided.
```

---

## 14. Step-by-Step Usage Examples

### Example 1: Single-Org Fabric-X Network

**Step 1: Prepare crypto material**

Generate MSP directories for your organization (using Fabric CA or cryptogen).

**Step 2: Write arma_shared_config.binpb**

This binary protobuf file defines the Arma party topology. It is generated by
your deployment tooling and contains `SharedConfig` with party definitions.

**Step 3: Write configtx.yaml**

```yaml
---
Organizations:
  - &MyOrg
    Name: MyOrg
    ID: MyOrg
    MSPDir: crypto/MyOrg/msp
    Policies:
      Readers:
        Type: Signature
        Rule: OR('MyOrg.member')
      Writers:
        Type: Signature
        Rule: OR('MyOrg.member')
      Admins:
        Type: Signature
        Rule: OR('MyOrg.admin')
      Endorsement:
        Type: Signature
        Rule: OR('MyOrg.member')
    OrdererEndpoints:
      - id=0,broadcast,orderer-1:7050
      - id=0,deliver,orderer-1:7060

Capabilities:
  Channel: &ChannelCapabilities
    V3_0: true
  Orderer: &OrdererCapabilities
    V2_0: true
  Application: &ApplicationCapabilities
    V2_5: true

Application: &ApplicationDefaults
  ACLs:
    _lifecycle/CommitChaincodeDefinition: /Channel/Application/Writers
    qscc/GetChainInfo:                   /Channel/Application/Readers
    peer/Propose:                        /Channel/Application/Writers
    event/Block:                         /Channel/Application/Readers
  Policies:
    LifecycleEndorsement:
      Type: ImplicitMeta
      Rule: MAJORITY Endorsement
    Endorsement:
      Type: ImplicitMeta
      Rule: MAJORITY Endorsement
    Readers:
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:
      Type: ImplicitMeta
      Rule: MAJORITY Admins
  Capabilities:
    <<: *ApplicationCapabilities

Orderer: &OrdererDefaults
  OrdererType: arma
  BatchTimeout: 2s
  BatchSize:
    MaxMessageCount: 500
    AbsoluteMaxBytes: 10 MB
    PreferredMaxBytes: 2 MB
  ConsenterMapping:
    - ID: 1
      Host: orderer-1
      Port: 7050
      MSPID: MyOrg
      Identity: crypto/MyOrg/msp/admincerts/Admin-cert.pem
      ClientTLSCert: crypto/MyOrg/msp/admincerts/Admin-cert.pem
      ServerTLSCert: crypto/MyOrg/msp/admincerts/Admin-cert.pem
  Arma:
    Path: arma_shared_config.binpb
  Policies:
    Readers:
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:
      Type: ImplicitMeta
      Rule: MAJORITY Admins
    BlockValidation:
      Type: ImplicitMeta
      Rule: MAJORITY Writers
  Capabilities:
    <<: *OrdererCapabilities

Channel: &ChannelDefaults
  Policies:
    Readers:
      Type: ImplicitMeta
      Rule: ANY Readers
    Writers:
      Type: ImplicitMeta
      Rule: ANY Writers
    Admins:
      Type: ImplicitMeta
      Rule: MAJORITY Admins
  Capabilities:
    <<: *ChannelCapabilities

Profiles:
  MyNetwork:
    <<: *ChannelDefaults
    Orderer:
      <<: *OrdererDefaults
      Organizations:
        - <<: *MyOrg
    Application:
      <<: *ApplicationDefaults
      Organizations:
        - <<: *MyOrg
```

**Step 4: Generate genesis block**

```bash
configtxgen \
  -profile MyNetwork \
  -channelID mynetwork \
  -outputBlock genesis.block \
  -configPath .
```

**Step 5: Verify**

```bash
configtxgen -inspectBlock genesis.block
```

**Step 6: Distribute genesis.block to all nodes and start the network.**

---

### Example 2: Two-Org Fabric-X Network

```yaml
---
Organizations:
  - &Org1
    Name: Org1
    ID: Org1
    MSPDir: crypto/Org1/msp
    Policies:
      Readers:
        Type: Signature
        Rule: OR('Org1.member')
      Writers:
        Type: Signature
        Rule: OR('Org1.member')
      Admins:
        Type: Signature
        Rule: OR('Org1.admin')
      Endorsement:
        Type: Signature
        Rule: OR('Org1.member')
    OrdererEndpoints:
      - id=0,broadcast,localhost:7050
      - id=0,deliver,localhost:7060

  - &Org2
    Name: Org2
    ID: Org2
    MSPDir: crypto/Org2/msp
    Policies:
      Readers:
        Type: Signature
        Rule: OR('Org2.member')
      Writers:
        Type: Signature
        Rule: OR('Org2.member')
      Admins:
        Type: Signature
        Rule: OR('Org2.admin')
      Endorsement:
        Type: Signature
        Rule: OR('Org2.member')
    OrdererEndpoints:
      - id=1,broadcast,localhost:7051
      - id=1,deliver,localhost:7061

# ... (Capabilities, Application, Orderer, Channel sections as above) ...

Profiles:
  TwoOrgNetwork:
    <<: *ChannelDefaults
    Orderer:
      <<: *OrdererDefaults
      OrdererType: arma
      ConsenterMapping:
        - ID: 1
          Host: orderer-1
          Port: 7050
          MSPID: Org1
          Identity: crypto/Org1/msp/admincerts/Admin@Org1-cert.pem
          ClientTLSCert: crypto/Org1/msp/admincerts/Admin@Org1-cert.pem
          ServerTLSCert: crypto/Org1/msp/admincerts/Admin@Org1-cert.pem
        - ID: 2
          Host: orderer-2
          Port: 7050
          MSPID: Org2
          Identity: crypto/Org2/msp/admincerts/Admin@Org2-cert.pem
          ClientTLSCert: crypto/Org2/msp/admincerts/Admin@Org2-cert.pem
          ServerTLSCert: crypto/Org2/msp/admincerts/Admin@Org2-cert.pem
      Arma:
        Path: arma_shared_config.binpb
      Policies:
        Readers:
          Type: ImplicitMeta
          Rule: ANY Readers
        Writers:
          Type: ImplicitMeta
          Rule: ANY Writers
        Admins:
          Type: ImplicitMeta
          Rule: MAJORITY Admins
        BlockValidation:
          Type: ImplicitMeta
          Rule: MAJORITY Writers
      Organizations:
        - <<: *Org1
        - <<: *Org2
    Application:
      <<: *ApplicationDefaults
      Organizations:
        - <<: *Org1
        - <<: *Org2
```

```bash
configtxgen \
  -profile TwoOrgNetwork \
  -channelID production \
  -outputBlock genesis.block \
  -configPath .
```

**What happens internally:**

```
Step 1: configtxgen reads configtx.yaml
Step 2: Looks up profile "TwoOrgNetwork"
Step 3: Resolves all YAML anchors (*Org1, *Org2, *ChannelDefaults, etc.)
Step 4: CompleteInitialization:
        - Validates Arma consenter mapping (2 consenters, all fields present)
        - Loads MSP certs for Org1 and Org2 from disk
        - Loads consenter identity + TLS certs from disk
        - Resolves arma_shared_config.binpb path
Step 5: Builds ConfigGroup hierarchy:
          Channel/
            +-- Orderer/
            |   +-- Org1/ (MSP + endpoints: id=0,broadcast/deliver)
            |   +-- Org2/ (MSP + endpoints: id=1,broadcast/deliver)
            |   +-- Orderers value (consenter protos with embedded certs)
            |   +-- ConsensusType value (type=arma, metadata=SharedConfig)
            +-- Application/
                +-- Org1/ (MSP + policies)
                +-- Org2/ (MSP + policies)
                +-- LifecycleEndorsement policy (MAJORITY Endorsement)
Step 6: Wraps ConfigGroup in ConfigEnvelope -> Envelope -> Block #0
Step 7: Writes genesis.block (protobuf binary, permissions 0640)
```

---

## 15. Troubleshooting

### Common Errors

```
+----------------------------------------------------------------------+
| Error                                 | Solution                     |
+-----------------------------------------------------------------------+
| "Error reading configuration:         | Set FABRIC_CFG_PATH or use   |
|  Unsupported Config Type"             | -configPath to point to dir  |
|                                       | containing configtx.yaml     |
+-----------------------------------------------------------------------+
| "Could not find profile: X"          | Profile name is case-         |
|                                       | sensitive. Check exact match. |
+-----------------------------------------------------------------------+
| "Missing channelID"                  | Add -channelID flag           |
+-----------------------------------------------------------------------+
| "global orderer endpoints exist,     | Remove Orderer.Addresses.     |
|  but can not be used with V3_0"      | Use per-org OrdererEndpoints. |
+-----------------------------------------------------------------------+
| "orderer endpoints for org X are     | Each orderer org MUST have    |
|  missing ... V3_0 is enabled"        | OrdererEndpoints defined.     |
+-----------------------------------------------------------------------+
| "consenter info did not specify       | ConsenterMapping entries need |
|  host / port / MSP ID / identity /   | all fields: ID, Host, Port,  |
|  client TLS cert / server TLS cert"  | MSPID, Identity,             |
|                                       | ClientTLSCert, ServerTLSCert |
+-----------------------------------------------------------------------+
| "X configuration did not specify      | ConsenterMapping must have   |
|  any consenter"                       | at least one entry for Arma. |
+-----------------------------------------------------------------------+
| "cannot load metadata for orderer     | Arma.Path must point to a    |
|  type arma"                           | valid binary protobuf file.  |
+-----------------------------------------------------------------------+
| "no BlockValidation policy defined"  | Add BlockValidation to       |
|                                       | Orderer.Policies section.    |
+-----------------------------------------------------------------------+
| "Error loading MSP configuration"    | Check MSPDir path exists     |
|                                       | and contains valid certs.    |
+-----------------------------------------------------------------------+
| "organization X is marked to be       | Set SkipAsForeign: false for |
|  skipped as foreign"                  | all orgs in genesis blocks.  |
+-----------------------------------------------------------------------+
| "unknown orderer type: X"            | Use: "arma", "etcdraft", or  |
|                                       | "BFT". Solo and Kafka are    |
|                                       | removed.                     |
+-----------------------------------------------------------------------+
```

### Pre-Flight Checklist

Before running configtxgen, verify:

```
[ ] configtx.yaml exists in FABRIC_CFG_PATH or -configPath directory
[ ] Profile name matches exactly (case-sensitive)
[ ] Channel capabilities include V3_0: true
[ ] No global Orderer.Addresses (use per-org OrdererEndpoints instead)
[ ] OrdererEndpoints defined for every orderer org
[ ] All MSPDir paths exist and contain valid crypto material
[ ] All TLS cert and identity paths in ConsenterMapping are valid
[ ] ConsenterMapping has at least one entry
[ ] Arma.Path points to a valid binary protobuf SharedConfig file
[ ] BlockValidation policy is defined in Orderer.Policies
[ ] Readers, Writers, and Admins policies defined at every level
[ ] -channelID is provided when generating genesis blocks
```
