# ParadeDB 存量库升级

当前 fork 的根目录生产与开发 Compose 已使用 `paradedb/paradedb:v0.25.11-pg17`；现有数据库仍须先备份并完成扩展升级，再恢复应用。本文处理 **0.22.x → 0.25.11、PostgreSQL 主版本保持 17** 的升级；不适用于 PostgreSQL 跨主版本升级，也不代表 Helm 中其他旧版本可以直接套用。配置范围见 [Compose 数据库说明](../../ictrek.app/README.md#compose-数据库版本与升级边界)。

该版本线相对 `v0.22.6-pg17` 带来 PostgreSQL `17.9` → `17.11`、pgvector（向量检索扩展）`0.8.1` → `0.8.4`，修复并行 HNSW（图结构向量索引）构建缓冲区溢出与 VACUUM（清理过期行）期间的索引损坏，见 [pgvector 变更记录](https://github.com/pgvector/pgvector/blob/v0.8.4/CHANGELOG.md)。pg_search（关键词检索扩展）从 `0.24.0` 起采用 WAL（预写日志）进行崩溃恢复，见 [paradedb#4901](https://github.com/paradedb/paradedb/pull/4901)。

> [!WARNING]
> 替换镜像后**必须**把扩展 catalog（SQL 定义与版本记录）升级到与镜像一致的版本。catalog 停留在旧版本时，部分扩展 SQL 函数（例如 `paradedb.version_info()`）会因签名与新二进制不匹配而触发段错误，导致整个 PostgreSQL 实例崩溃并进入恢复。也**不要**在新镜像上执行 `ALTER EXTENSION pg_search UPDATE TO '0.22.6'`，它同样会造成上述不匹配。

共享 VOS PGV（独立管理的数据库应用）`0.0.48` 已使用 PostgreSQL 17，版本核查与维护要求见 [HybRAG 运维说明](../../ictrek.app/README.md#pgv-bm25-索引升级与故障恢复)。VOS 的数据库容器由 PGV 应用管理，不套用本文根目录 Compose 命令；本地 ictrek 开发环境的旧 PostgreSQL 16 数据处理见 [开发镜像替换](../../ictrek.app/docs/local-dev/README.md#postgresql-镜像替换)。

## 升级步骤

所有命令在仓库根目录执行。开发环境的数据库命令使用 `docker compose -f docker-compose.dev.yml`，开发后端在宿主机上手动停止；开发 Compose 没有 `app` 服务。保留原数据卷，**不要执行 `down -v`、删除卷或换成 PG18 镜像**。

1. 停止 app 和其他数据库写入方；启用了 Langfuse 时也要停止其 web/worker，本地运行的开发后端同样需要停止。

   ```bash
   # 标准部署；开发环境请停止宿主机上的后端进程
   docker compose stop app
   # 仅在启用了 Langfuse 时执行
   docker compose stop langfuse-web langfuse-worker
   ```

2. 备份所有数据库和角色，包括 Langfuse 库，并确认能够恢复。备份存放在数据库数据卷之外。

   ```bash
   umask 077
   docker compose exec -T postgres sh -c 'pg_dumpall -U "$POSTGRES_USER"' > paradedb-before-upgrade.sql
   ```

   若旧镜像在当前 CPU 上无法启动，先保存停止状态的数据卷快照，在兼容机器上制作逻辑备份或验证快照可恢复，再改动唯一的数据副本。

3. 决定升级时，先将所用 Compose 中 `postgres` 和 `langfuse-db-init` 的镜像均改为 `paradedb/paradedb:v0.25.11-pg17`，再只替换数据库容器：

   ```bash
   docker compose pull postgres
   docker compose up -d --no-deps --wait postgres
   ```

4. 完成扩展的 SQL 升级。仅替换镜像不会更新已有数据库内的 `pg_extension` 版本。新镜像自带 0.22.x 起的完整升级脚本链，任意 0.22.x 都可以一步升级到镜像默认版本：

   ```bash
   docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1' <<'SQL'
   ALTER EXTENSION pg_search UPDATE;
   ALTER EXTENSION vector UPDATE;
   SELECT extname, extversion FROM pg_extension WHERE extname IN ('pg_search', 'vector');
   SELECT * FROM paradedb.version_info();
   SQL
   ```

   在**其他已安装 `pg_search` 或 `vector` 的数据库**中同样执行，包括安装过该扩展的 `postgres`、模板库或 Langfuse 库。不要为了升级而向原本不需要的数据库安装扩展。`pg_search` 的 catalog 与 `paradedb.version_info()` 应均报告 0.25.11，`vector` 应为 0.8.4。

5. 恢复原先运行的写入服务，确认数据库迁移状态，执行有代表性的关键词与向量检索。已有的 BM25 与 HNSW 索引在新版本下可继续读写，无需专门重建全部索引或重新导入文档；如遇异常可 `REINDEX`。保留备份直到验证完成。

## 自动迁移的范围

WeKnora 启动时的迁移只在 WeKnora 数据库内升级扩展，遵守 `app.skip_embedding`，不安装缺失的扩展，也不降级或处理其他版本线：

- `000137`：已安装的 `pg_search` 为 0.22.0–0.25.10 且服务器提供 0.25.11 时，升级到 0.25.11；已安装的 `vector` 为 0.8.0–0.8.3 且服务器提供 0.8.4 时，升级到 0.8.4。
- `000117`：历史迁移，仅在服务器提供 0.22.6 扩展包（即仍使用 `v0.22.6-pg17` 镜像）时把 0.22.2–0.22.5 升级到 0.22.6；新镜像上会跳过，交给 `000137`。

迁移 `000117`、`000137` 遇到扩展权限不足时记录管理员处理提示，继续应用迁移，不自动重建索引。共享数据库须由管理员确认镜像、扩展版本和索引相容后恢复业务。

如果这些迁移在替换数据库镜像**之前**已经执行，安装新镜像后不会自动再跑一次，需要手动执行上面的 SQL。没有对应迁移的旧应用也需手工升级。其他数据库的扩展不能靠 WeKnora 的迁移代管。

## 回滚与复现验证

回滚只能将升级前备份/快照恢复到独立数据卷，配合旧镜像使用。仅把镜像标签改回旧版不会撤销扩展 SQL 变更；而且 pg_search 0.24 起会向 WAL 写入自定义资源管理器记录，旧镜像无法重放这些记录。迁移的 down 文件也不会尝试降级扩展。

仓库提供隔离验证脚本，默认验证 0.22.2 → 0.25.11，也可用 `--to 0.22.6` 验证历史补丁升级：

```bash
docker pull paradedb/paradedb:v0.22.2-pg17
docker pull paradedb/paradedb:v0.25.11-pg17
python3 scripts/test_paradedb_upgrade.py
```

脚本使用临时容器和卷，不映射端口；验证同一数据卷升级、表内容、关键词/向量检索、迁移幂等性和重启后结果，并输出备份与日志。该用例验证固定测试数据，部署时仍需检查自己的数据和检索负载。
