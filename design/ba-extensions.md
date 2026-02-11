# OpenStack BA/ACP Extensions (Draft)

## 背景

Cluster API 仓库中的 Ansible Bootstrap（BA）与 Ansible Control Plane（ACP）已经实现完毕，当前缺口在于基础设施 Provider（CAPO）需要补足一组 OpenStack 特有的变量和状态，供 bootstrap controller 渲染 `inventory.ini` 与 `vars.yaml`。为避免直接向现有 `spec`/`status` 注入业务字段，我们计划引入 `extensions`（暂定名）作为结构化挂载点。

## 目标

1. **最小侵入**：保持 `OpenStackCluster{Spec,Status}` 与 `OpenStackMachine{Spec,Status}` 的核心 schema 不变。
2. **清晰的读写边界**：由 infra controller 写入 `status.extensions`，bootstrap/ACP controller 只读取；敏感信息通过 Secret/引用传递。
3. **渐进式演进**：先落地最关键的 BA/ACP 依赖字段，未来可以按需扩展。

## API 形态（示例）

```go
type OpenStackClusterSpec struct {
    ...
    Extensions *OpenStackClusterExtensionsSpec `json:"extensions,omitempty"`
}

type OpenStackClusterStatus struct {
    ...
    Extensions *OpenStackClusterExtensionsStatus `json:"extensions,omitempty"`
}
```

`OpenStackMachine{Spec,Status}` 同理。Extensions 可先按领域拆分为 `Networking`, `LoadBalancers`, `CredentialsRef`, `Resources` 等子结构，必要时使用 `apiextensionsv1.JSON` 作为兜底以便快速迭代。

## 字段清单（完全对齐 BA README）

| README 字段 | Infra Type | 建议 Extensions 路径                                         | 数据来源/写入方                                               | 消费方及用途                           |
|-------------|------------|----------------------------------------------------------|--------------------------------------------------------|----------------------------------|
| `kube_network_plugin` | Cluster | `spec.extensions.networking.kubeNetworkPlugin`           | 用户/ClusterClass                                        | BA 渲染 vars，决定使用 cilium/flannel 等 |
| `cilium_openstack_project_id` | Cluster | `status.extensions.networking.cilium.projectID`          | CAPO：根据网络/项目配置写入                                       | BA/ACP 配置 Cilium 与 OpenStack 集成  |
| `cilium_openstack_default_subnet_id` | Cluster | `status.extensions.networking.cilium.defaultSubnetID`    | CAPO                                                   | BA 提供给 VPC CNI                   |
| `cilium_openstack_security_group_ids` | Cluster | `status.extensions.networking.cilium.securityGroupIDs[]` | CAPO                                                   | BA 配置 Pod/节点安全组                  |
| `vpc_cni_webhook_enable` | Cluster | `status.extensions.networking.cilium.webhookEnable`      | CAPO 根据网络特性写入                                          | BA 控制是否部署相关 webhook              |
| `master_virtual_vip` | Cluster | `status.extensions.loadBalancers.controlPlane.vip`       | CAPO（API Server LB/VIP）                                | BA Inventory/vars                |
| `ingress_virtual_vip` | Cluster | `status.extensions.loadBalancers.ingress.vip`            | CAPO（Ingress LB）                                       | BA/Harbor                        |
| `keepalived_interface` | Machine | `spec.extensions.networkInterfaces.keepalived`           | 用户/ClusterClass 通过 Machine 模板指定                         | BA 设置 keepalived                 |
| `harbor_addr` | Cluster | `status.extensions.loadBalancers.ingress.vip`            | CAPO/用户                                                | BA 渲染 控制面harbor入口ingress vip     |
| `cloud_master_vip` | Cluster | `status.extensions.openStack.mgmt`                       | CAPO（管理网络 公网IP）                                        | BA/外部访问                          |
| `openstack_auth_domain` | Cluster | `status.extensions.openStack.keystone`                   | CAPO（从 IdentityRef 解出只读 endpoint）                      | BA vars；凭证仍通过 SecretRef          |
| `openstack_cinder_domain` | Cluster | `status.extensions.openStack.cinder`                     | CAPO                                                   | BA/存储插件                          |
| `openstack_nova_domain` | Cluster | `status.extensions.openStack.nova`                       | CAPO                                                   | BA/Cloud provider 配置             |
| `openstack_neutron_domain` | Cluster | `status.extensions.openStack.neutron`                    | CAPO                                                   | BA/网络插件                          |
| `openstack_project_name` | Cluster | `status.extensions.openStack.project`                    | CAPO                                                   | BA vars                          |
| `openstack_project_domain_name` | Cluster | `status.extensions.openStack.projectDomain`              | CAPO                                                   | BA vars                          |
| `openstack_region_name` | Cluster | `status.extensions.openStack.region`                     | CAPO                                                   | BA vars                          |
| `ntp_server` | Cluster | `status.extensions.platform.ntp.server`                  | CAPO（基础设施/跳板配置） 和bastion一致                             | BA hosts/vars                    |
| `vip_mgmt` | Cluster | `status.extensions.platform.management.vip`              | CAPO（跳板机）                                              | BA/运维脚本                          |
| `flannel_interface` | Cluster | `spec.extensions.networkInterfaces.flannel`              | 用户/ClusterClass（集群唯一配置）                             | BA vars                          |
| `node_resources.<machine>` | Machine | `status.extensions.nodeResources.reserved`               | CAPO（根据 flavor/策略）  代表此批配置预留                           | BA 生成 `node_resources` 字段        |

> 说明：`openstack_*` 用户名/密码等敏感信息不应出现在 status；表中“建议路径”表示 status 可记录非敏感属性，如 endpoint、projectName 等。OpenStack 凭据将通过 cloud-init/模板注入，不经 vars renderer 暴露。

## 数据流与职责

1. **状态类字段（status.extensions.\*)**  
   - 由 CAPO 各 controller（Cluster/Machine 服务）在 reconcile 时写入，包含网络、负载均衡、平台 Endpoint、节点资源等信息。  
   - BA/ACP 只读这些字段，用来渲染 `inventory.ini`、`vars.yaml`。  
   - 如需回填 VIP 等 Infra 输出，应保证 CAPO 在对象就绪前设置 Condition，不让 BA 消耗半成品数据。

2. **配置/业务类字段（spec.extensions.bootstrapVars.\*)**  
   - 用户/ClusterClass 通过 YAML 注入，CAPO 不解释含义，只做格式校验并存储。  
   - BA/ACP 合并该 map，优先级高于默认值，实现“业务层可覆盖 infra 默认”。

3. **凭据引用**  
   - App Credential 等敏感信息不通过 extensions 字段暴露。  
   - status 仅记录只读的 endpoint/project 信息，防止敏感数据泄露。

### 典型流程

1. **Control Plane VIP**  
   - CAPO 在创建或发现 API Server LB 后，把 VIP 写入 `status.extensions.loadBalancers.controlPlane.vip`。  
   - BA 在 `postBootstrap` 阶段读取该值，注入 `master_virtual_vip`，并将其同步到 kubean/kubespray inventory。

2. **Ingress VIP 与 Harbor 地址**  
   - CAPO 负责分配/维护 Ingress 层的虚拟 IP，写入 `status.extensions.loadBalancers.ingress.vip`。  
   - BA/ACP 将该值用作 `ingress_virtual_vip` 及 Harbor 暴露地址的默认值。

3. **网络接口/资源上报**  
   - CAPO reconcile Machine 后，根据实例实际网卡名与预留资源把结果写进 `status.extensions.networkInterfaces` 与 `status.extensions.nodeResources`。  
   - BA/ACP 在生成 vars 时引用这些字段，确保 keepalived、flannel、资源预留参数与真实环境一致。

4. **自定义 Bootstrap Vars**  
   - 用户/ClusterClass 在 `spec.extensions.bootstrapVars` 中配置 `kube_network_plugin` 等通用参数。  
   - CAPO 不解析具体内容，仅负责存储与下发；BA/ACP merge 这些键值形成最终 `vars.yaml`。

## 下一步

1. 设计 `Extensions` 结构的具体字段与 CRD 校验（若使用 struct）。(TODO: 通过 `api/v1beta1/extensions_types.go` 引入类型)。
2. 在 controller 中实现读写逻辑：Status 字段由 CAPO 设置，Spec 字段通过 webhook 验证。（TODO: 通过 `controllers/extensions_reconcile.go` 单独处理）
3. 更新 README/Book，说明 extensions 使用方式及与 BA 的交互。

> 注：本设计文档为初稿，后续根据实现细节可拆分为正式的 CAPO proposal。
