import { useState, useEffect } from "react";
import {
  Users,
  Plus,
  Search,
  MoreHorizontal,
  Edit,
  Trash2,
  Shield,
  Mail,
  AlertCircle,
  RefreshCw,
  Key,
  HardDrive,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { Progress } from "@/components/ui/progress";
import { useAccounts } from "@/hooks/useApi";
import { cn } from "@/lib/utils";
import type { Account } from "@/types";

export function Accounts() {
  const {
    accounts,
    loading,
    error: _error,
    fetchAccounts,
    createAccount,
    updateAccount,
    deleteAccount,
  } = useAccounts();

  const [searchQuery, setSearchQuery] = useState("");
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);
  const [selectedAccount, setSelectedAccount] = useState<Account | null>(null);
  const [newAccountEmail, setNewAccountEmail] = useState("");
  const [newAccountPassword, setNewAccountPassword] = useState("");
  const [newAccountIsAdmin, setNewAccountIsAdmin] = useState(false);
  const [formError, setFormError] = useState("");

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  const filteredAccounts = accounts?.filter((a: Account) =>
    a.email.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const handleCreateAccount = async () => {
    setFormError("");
    if (!newAccountEmail || !newAccountPassword) {
      setFormError("Email and password are required");
      return;
    }

    try {
      await createAccount(newAccountEmail, newAccountPassword, newAccountIsAdmin);
      setIsAddDialogOpen(false);
      setNewAccountEmail("");
      setNewAccountPassword("");
      setNewAccountIsAdmin(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to create account");
    }
  };

  const handleDeleteAccount = async () => {
    if (!selectedAccount) return;

    try {
      await deleteAccount(selectedAccount.email);
      setIsDeleteDialogOpen(false);
      setSelectedAccount(null);
    } catch (err) {
      console.error("Failed to delete account:", err);
    }
  };

  const handleUpdateAccount = async () => {
    if (!selectedAccount) return;

    try {
      const updates: Partial<Account> & { password?: string } = {
        is_admin: selectedAccount.is_admin,
        is_active: selectedAccount.is_active,
      };
      if (newAccountPassword) {
        updates.password = newAccountPassword;
      }
      await updateAccount(selectedAccount.email, updates);
      setIsEditDialogOpen(false);
      setSelectedAccount(null);
      setNewAccountPassword("");
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to update account");
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Accounts</h1>
          <p className="text-muted-foreground mt-1">
            Manage email accounts and permissions
          </p>
        </div>
        <Dialog open={isAddDialogOpen} onOpenChange={setIsAddDialogOpen}>
          {/* @ts-expect-error asChild prop not typed in Base UI but works at runtime */}
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Account
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Create New Account</DialogTitle>
              <DialogDescription>
                Create a new email account on your server.
              </DialogDescription>
            </DialogHeader>
            {formError && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription>{formError}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="email">Email Address</Label>
                <Input
                  id="email"
                  type="email"
                  placeholder="user@example.com"
                  value={newAccountEmail}
                  onChange={(e) => setNewAccountEmail(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  placeholder="••••••••"
                  value={newAccountPassword}
                  onChange={(e) => setNewAccountPassword(e.target.value)}
                />
              </div>
              <div className="flex items-center justify-between pt-2">
                <Label htmlFor="is-admin">Admin Account</Label>
                <Switch
                  id="is-admin"
                  checked={newAccountIsAdmin}
                  onCheckedChange={setNewAccountIsAdmin}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setIsAddDialogOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreateAccount}>Create Account</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Search and Filter */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search accounts..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-10"
          />
        </div>
        <Button
          variant="outline"
          size="icon"
          onClick={() => fetchAccounts()}
          disabled={loading}
        >
          <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
        </Button>
      </div>

      {/* Accounts List */}
      {loading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <Card key={i}>
              <CardContent className="p-6">
                <Skeleton className="h-6 w-3/4 mb-4" />
                <Skeleton className="h-4 w-1/2" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : filteredAccounts?.length === 0 ? (
        <Card className="text-center py-12">
          <CardContent>
            <Users className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="text-lg font-medium">No accounts found</h3>
            <p className="text-muted-foreground mt-1">
              {searchQuery
                ? "No accounts match your search"
                : "Get started by adding your first account"}
            </p>
            {!searchQuery && (
              <Button className="mt-4" onClick={() => setIsAddDialogOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Add Account
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {filteredAccounts?.map((account: Account) => (
            <AccountCard
              key={account.email}
              account={account}
              onEdit={() => {
                setSelectedAccount(account);
                setIsEditDialogOpen(true);
              }}
              onDelete={() => {
                setSelectedAccount(account);
                setIsDeleteDialogOpen(true);
              }}
              formatBytes={formatBytes}
            />
          ))}
        </div>
      )}

      {/* Edit Account Dialog */}
      <Dialog open={isEditDialogOpen} onOpenChange={setIsEditDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Edit Account</DialogTitle>
            <DialogDescription>
              Update account settings for {selectedAccount?.email}
            </DialogDescription>
          </DialogHeader>
          {formError && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          {selectedAccount && (
            <div className="space-y-4 py-4">
              <div className="flex items-center justify-between">
                <Label htmlFor="edit-is-admin">Admin Account</Label>
                <Switch
                  id="edit-is-admin"
                  checked={selectedAccount.is_admin}
                  onCheckedChange={(checked) =>
                    setSelectedAccount({ ...selectedAccount, is_admin: checked })
                  }
                />
              </div>
              <div className="flex items-center justify-between">
                <Label htmlFor="edit-is-active">Active</Label>
                <Switch
                  id="edit-is-active"
                  checked={selectedAccount.is_active}
                  onCheckedChange={(checked) =>
                    setSelectedAccount({ ...selectedAccount, is_active: checked })
                  }
                />
              </div>
              <div className="space-y-2 pt-4 border-t">
                <Label htmlFor="new-password">New Password (optional)</Label>
                <Input
                  id="new-password"
                  type="password"
                  placeholder="Leave empty to keep current"
                  value={newAccountPassword}
                  onChange={(e) => setNewAccountPassword(e.target.value)}
                />
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsEditDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleUpdateAccount}>Save Changes</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={isDeleteDialogOpen} onOpenChange={setIsDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Account</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete {selectedAccount?.email}? This action
              cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteAccount}>
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface AccountCardProps {
  account: Account;
  onEdit: () => void;
  onDelete: () => void;
  formatBytes: (bytes: number) => string;
}

function AccountCard({ account, onEdit, onDelete, formatBytes }: AccountCardProps) {
  const quotaPercent = account.quota_limit > 0
    ? Math.round((account.quota_used / account.quota_limit) * 100)
    : 0;

  return (
    <Card className="group">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className={cn(
              "p-2 rounded-lg",
              account.is_admin
                ? "bg-gradient-to-br from-violet-500 to-violet-600"
                : "bg-gradient-to-br from-blue-500 to-blue-600"
            )}>
              {account.is_admin ? (
                <Shield className="h-5 w-5 text-white" />
              ) : (
                <Mail className="h-5 w-5 text-white" />
              )}
            </div>
            <div className="min-w-0">
              <CardTitle className="text-base truncate">{account.email}</CardTitle>
              <CardDescription className="flex items-center gap-1">
                {account.is_admin && (
                  <Badge variant="secondary" className="text-xs">Admin</Badge>
                )}
                {!account.is_active && (
                  <Badge variant="destructive" className="text-xs">Inactive</Badge>
                )}
              </CardDescription>
            </div>
          </div>
          <DropdownMenu>
            {/* @ts-expect-error asChild prop not typed in Base UI but works at runtime */}
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={onEdit}>
                <Edit className="mr-2 h-4 w-4" />
                Edit
              </DropdownMenuItem>
              {account.totp_enabled && (
                <DropdownMenuItem disabled>
                  <Key className="mr-2 h-4 w-4" />
                  2FA Enabled
                </DropdownMenuItem>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onDelete} className="text-red-600">
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {/* Quota Usage */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground flex items-center gap-1">
                <HardDrive className="h-4 w-4" />
                Storage
              </span>
              <span className="font-medium">
                {formatBytes(account.quota_used)} / {formatBytes(account.quota_limit)}
              </span>
            </div>
            <Progress
              value={quotaPercent}
              className="h-2"
            />
            <p className="text-xs text-muted-foreground text-right">
              {quotaPercent}% used
            </p>
          </div>

          {/* Last Login */}
          {account.last_login && (
            <div className="text-xs text-muted-foreground pt-2 border-t">
              Last login: {new Date(account.last_login).toLocaleDateString()}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
