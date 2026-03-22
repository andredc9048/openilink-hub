import { useEffect, useState } from "react";
import { Button } from "../components/ui/button";
import { Card } from "../components/ui/card";
import { api } from "../lib/api";
import { Link2, Unlink } from "lucide-react";

const providerLabels: Record<string, string> = {
  github: "GitHub",
  linuxdo: "LinuxDo",
};

export function SettingsPage() {
  const [user, setUser] = useState<any>(null);
  const [oauthAccounts, setOauthAccounts] = useState<any[]>([]);
  const [oauthProviders, setOauthProviders] = useState<string[]>([]);

  async function load() {
    const [u, accounts, providers] = await Promise.all([
      api.me(),
      api.oauthAccounts(),
      api.oauthProviders(),
    ]);
    setUser(u);
    setOauthAccounts(accounts || []);
    setOauthProviders(providers.providers || []);
  }

  useEffect(() => { load(); }, []);

  // Check for OAuth callback results in URL
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("oauth_bound") || params.get("oauth_error")) {
      // Clean URL
      window.history.replaceState({}, "", "/settings");
      load();
    }
  }, []);

  async function handleUnlink(provider: string) {
    if (!confirm(`确认解绑 ${providerLabels[provider] || provider}？`)) return;
    try {
      await api.unlinkOAuth(provider);
      load();
    } catch (err: any) {
      alert(err.message);
    }
  }

  function handleBind(provider: string) {
    window.location.href = `/api/auth/oauth/${provider}/bind`;
  }

  if (!user) return null;

  const linkedProviders = new Set(oauthAccounts.map((a) => a.provider));

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">设置</h2>

      {/* Account info */}
      <Card className="space-y-3">
        <h3 className="text-sm font-medium">账号信息</h3>
        <div className="text-sm space-y-1">
          <p><span className="text-[var(--muted-foreground)]">用户名：</span>{user.username}</p>
          <p><span className="text-[var(--muted-foreground)]">显示名：</span>{user.display_name}</p>
          <p><span className="text-[var(--muted-foreground)]">角色：</span>{user.role}</p>
        </div>
      </Card>

      {/* OAuth accounts */}
      {oauthProviders.length > 0 && (
        <Card className="space-y-3">
          <h3 className="text-sm font-medium">第三方账号绑定</h3>
          <div className="space-y-2">
            {oauthProviders.map((provider) => {
              const account = oauthAccounts.find((a) => a.provider === provider);
              const linked = !!account;

              return (
                <div
                  key={provider}
                  className="flex items-center justify-between p-3 rounded-lg border border-[var(--border)] bg-[var(--background)]"
                >
                  <div className="flex items-center gap-3">
                    <div className="w-8 h-8 rounded-full bg-[var(--secondary)] flex items-center justify-center">
                      <span className="text-xs font-medium">
                        {(providerLabels[provider] || provider).charAt(0).toUpperCase()}
                      </span>
                    </div>
                    <div>
                      <p className="text-sm font-medium">{providerLabels[provider] || provider}</p>
                      {linked ? (
                        <p className="text-xs text-[var(--muted-foreground)]">
                          已绑定：{account.username}
                        </p>
                      ) : (
                        <p className="text-xs text-[var(--muted-foreground)]">未绑定</p>
                      )}
                    </div>
                  </div>
                  {linked ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleUnlink(provider)}
                    >
                      <Unlink className="w-3.5 h-3.5 mr-1" /> 解绑
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleBind(provider)}
                    >
                      <Link2 className="w-3.5 h-3.5 mr-1" /> 绑定
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        </Card>
      )}
    </div>
  );
}
