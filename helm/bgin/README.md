# bgin Helm Chart

`bgin` 是一个企业级高并发 RESTful Web API 骨架的 Kubernetes 生产就绪 Helm Chart。

---

## 架构拓扑与特性

- **API 服务 (Deployment + Service + HPA + Ingress)**：无状态高性能 Web API 实例，内置 `/livez` 存活与 `/readyz` 就绪健康探测探针。
- **异步队列消费 (Worker Deployment)**：独立轻量异步工作进程，执行后台队列消费任务（`queue:work`）。
- **数据库迁移 (Migration Batch Job)**：通过 Helm 生命周期 Hook（`post-install,pre-upgrade`）在版本升级前自动完成 Atlas 声明式 DDL 迁移。
- **配置与敏感信息解耦 (ConfigMap & Secret)**：
  - 非敏感环境变量通过 `values.yaml` 中的 `env` 注入 ConfigMap；
  - 敏感信息支持内置 Secret、外部 `existingSecret`，以及 **SOPS + age** 密文 GitOps 模式。
- **开箱即用子组件 (Subcharts)**：内置可选的 Bitnami PostgreSQL (`16.4.15`) 与 Bitnami Valkey (`6.3.4`)，适用于测试或自建集群环境。

---

## 快速上手与常用 Just 指令

所有常规 Helm 任务均通过根目录 `justfile` 统一封装执行：

```bash
# 1. 静态代码语法检查
rtk just helm-lint

# 2. 默认参数渲染测试
rtk just helm-template

# 3. 更新/拉取子组件依赖 (Chart.lock)
rtk just helm-dep

# 4. 打包 Chart 至 helm/dist/ 目录
rtk just helm-package

# 5. 登录 CNB 或自定义 OCI 制品库
rtk just helm-login <token> [user] [registry]

# 6. 推送 Chart 至 OCI 制品库 (默认为 helm.cnb.cool/ailuoga/gin-bun/bgin)
rtk just helm-push 1.0.0
```

---

## 敏感信息管理：SOPS + age GitOps 工作流

生产敏感配置（如数据库账号、JWT 密钥、第三方凭据等）采用 **SOPS + age** 进行全生命周期加密管理，密文随代码库版本追溯。

### 1. 准备本地密钥与公钥配置

首次使用请生成本地 age 私钥：

```bash
rtk just sops-init
```

输出将提示本地公钥，例如 `age1dedh...`。将此公钥填入或追加至仓库根目录的 `.sops.yaml` 中（公钥可安全提交至 Git）。

### 2. 创建或编辑目标环境密文

从示例模板复制未加密文件：

```bash
cp helm/bgin/secrets.example.yaml helm/bgin/secrets.prod.yaml
# 编辑填入真实环境凭证后加密：
rtk just sops-encrypt prod
```

生成的 `helm/bgin/secrets.prod.enc.yaml` 为仅包含加密字段的密文文件，**可安全提交至 Git 仓库**。

日常修改环境密文，可直接通过交互式命令原地解密与自动重加密保存：

```bash
rtk just sops-edit prod
```

### 3. 本地无明文渲染测试

使用 `helm-template-env` 可以在内存中无感解密并验证最终渲染结果，**绝对不会在本地磁盘残留明文文件**：

```bash
rtk just helm-template-env prod
```

### 4. 生产流水线与 GitOps 交付

- **CNB / GitHub Actions 流水线部署**：
  在平台凭证管理中配置 Secret 环境变量 `SOPS_AGE_KEY`（私钥内容）。在部署阶段解密并传参：

  ```bash
  sops exec-file helm/bgin/secrets.prod.enc.yaml \
    'helm upgrade -i bgin ./helm/bgin -f ./helm/bgin/values.yaml -f {}'
  ```
- **ArgoCD / Flux 集群端部署**：
  在集群内配置 `helm-secrets` 插件，并将 age 私钥挂载至 ArgoCD Repo Server 容器中的 `/helm-secrets/key.txt`，ArgoCD 将自动拉取 Git 中的 `secrets.prod.enc.yaml` 并实时解密发布。

---

## 核心配置参数说明 (values.yaml)

| 参数键路径                  | 默认值                              | 作用描述                                                                   |
| :-------------------------- | :---------------------------------- | :------------------------------------------------------------------------- |
| `image.repository`        | `docker.cnb.cool/ailuoga/gin-bun` | 应用主容器镜像仓库                                                         |
| `image.tag`               | `""`                              | 镜像版本 Tag（留空时默认取`Chart.AppVersion`）                           |
| `api.enabled`             | `true`                            | 是否启用 Web API 实例                                                      |
| `api.replicaCount`        | `2`                               | Web API 副本数                                                             |
| `api.autoscaling.enabled` | `false`                           | 是否启用 API 水平自动伸缩 (HPA)                                            |
| `worker.enabled`          | `true`                            | 是否启用后台队列工作进程                                                   |
| `migration.enabled`       | `true`                            | 是否在发布时运行数据库版本迁移 Job                                         |
| `migration.hook`          | `"post-install,pre-upgrade"`      | 迁移 Hook 生命周期时机（外接托管 RDS 建议改为`pre-install,pre-upgrade`） |
| `existingSecret`          | `""`                              | 外部预置 Secret 名称（指定后 Chart 不再创建内置 Secret）                   |
| `secrets`                 | `{...}`                           | 内置敏感环境变量字典（由 SOPS 密文文件覆盖）                               |
| `postgresql.enabled`      | `false`                           | 是否部署 Chart 内置 PostgreSQL 子组件（仅供开发测试使用）                  |
| `valkey.enabled`          | `false`                           | 是否部署 Chart 内置 Valkey 缓存子组件（仅供开发测试使用）                  |
