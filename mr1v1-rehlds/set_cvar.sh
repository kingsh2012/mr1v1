#!/bin/bash
# 修改 cstrike/server.cfg 中某个cvar的值：已存在则覆盖（保留行尾注释），不存在则追加新行
# Usage: ./set_cvar.sh <cvar_name> <new_value> [cfg_file]
# 示例:  ./set_cvar.sh mp_roundtime 1.5
#        ./set_cvar.sh mp_autokick 0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CVAR="${1:?用法: $0 <cvar_name> <new_value> [cfg_file]}"
VALUE="${2:?用法: $0 <cvar_name> <new_value> [cfg_file]}"
CFG="${3:-$SCRIPT_DIR/cstrike/server.cfg}"

# 转义sed替换部分中的特殊字符（\、&、分隔符|），避免VALUE里出现这些字符时出错
ESCAPED_VALUE=$(printf '%s' "$VALUE" | sed -e 's/[\&|]/\\&/g')

if grep -qE "^${CVAR}[[:space:]]+\"" "$CFG"; then
  sed -i -E "s|^(${CVAR}[[:space:]]+)\"[^\"]*\"|\1\"${ESCAPED_VALUE}\"|" "$CFG"
  echo "已覆盖 ${CVAR} = \"${VALUE}\" (${CFG})"
else
  printf '%s "%s"\n' "$CVAR" "$VALUE" >> "$CFG"
  echo "已新增 ${CVAR} = \"${VALUE}\" (${CFG})"
fi
