"use client";

import * as React from "react";
import { EyeIcon, EyeOffIcon } from "lucide-react";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

// 密钥输入框：看着像密码框，但刻意不用 type="password"。
//
// 浏览器的密码管理器只认 type="password" 字段。ARTEX 的登录页、LLM 的 API Key、
// MCP 同步的 API Key 都在同一个源下，Chrome 于是把它们归成同一个登录表单：
// 在任何一处点「保存」都会弹「要更新密码吗？」，并把已存的那组凭据（其实是 ARTEX
// 的登录密码）当作默认值填进密钥框。本项目已经因此把一段 11 位的旧密码存成过 LLM
// 的 API Key，症状是此后每次「测试连接 / 获取模型」都 401，用起来像"每次都要重输一遍 Key"。
//
// autocomplete="off" 和 "new-password" 都挡不住那个保存/更新提示，唯一可靠的做法是让
// 密码管理器根本看不到 password 字段：这里用 type="text" + CSS 遮字(-webkit-text-security)。
// 遮字只有 Chromium 系支持，其余浏览器会明文显示，所以配了个眼睛按钮随时核对 ——
// 顺带解决"存进去的到底是不是垃圾"看不出来的问题。
function SecretInput({ className, ...props }: React.ComponentProps<"input">) {
  const [revealed, setRevealed] = React.useState(false);
  return (
    <div className="relative min-w-0 flex-1">
      <Input
        {...props}
        type="text"
        autoComplete="off"
        spellCheck={false}
        className={cn("pr-8", !revealed && "secret-mask", className)}
      />
      <button
        type="button"
        tabIndex={-1}
        aria-label={revealed ? "隐藏" : "显示"}
        onClick={() => setRevealed((v) => !v)}
        className="text-muted-foreground hover:text-foreground absolute inset-y-0 right-0 flex w-8 items-center justify-center"
      >
        {revealed ? <EyeOffIcon className="size-3.5" /> : <EyeIcon className="size-3.5" />}
      </button>
    </div>
  );
}

export { SecretInput };
