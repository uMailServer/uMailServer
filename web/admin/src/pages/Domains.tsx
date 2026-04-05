import { useState, useEffect } from "react";
import {
  Globe,
  Plus,
  Search,
  MoreHorizontal,
  Edit,
  Trash2,
  Copy,
  Check,
  AlertCircle,
  RefreshCw,
  Shield,
  Mail,
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
import { useDomains } from "@/hooks/useApi";
import { cn } from "@/lib/utils";
import type { Domain } from "@/types";

export function Domains() {
  const {
    domains,
    loading,
    error,
    fetchDomains,
    createDomain,
    updateDomain,
    deleteDomain,
  } = useDomains();

  const [searchQuery, setSearchQuery] = useState("");
  const [isAddDialogOpen, setIsAddDialogOpen] = useState(false);
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [selectedDomain, setSelectedDomain] = useState<Domain | null>(null);
  const [newDomainName, setNewDomainName] = useState("");
  const [newDomainMaxAccounts, setNewDomainMaxAccounts] = useState(100);
  const [formError, setFormError] = useState("");
  const [copiedDNS, setCopiedDNS] = useState(false);

  useEffect(() => {
    fetchDomains();
  }, [fetchDomains]);

  const filteredDomains = domains?.filter((d: Domain) =>
    d.name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const handleCreateDomain = async () => {
    setFormError("");
    if (!newDomainName) {
      setFormError("Domain name is required");
      return;
    }

    try {
      await createDomain(newDomainName, newDomainMaxAccounts);
      setIsAddDialogOpen(false);
      setNewDomainName("");
      setNewDomainMaxAccounts(100);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to create domain");
    }
  };

  const handleDeleteDomain = async () => {
    if (!selectedDomain) return;

    try {
      await deleteDomain(selectedDomain.name);
      setIsDeleteDialogOpen(false);
      setSelectedDomain(null);
    } catch (err) {
      console.error("Failed to delete domain:", err);
    }
  };

  const handleToggleDomain = async (domain: Domain) => {
    try {
      await updateDomain(domain.name, { is_active: !domain.is_active });
    } catch (err) {
      console.error("Failed to update domain:", err);
    }
  };

  const generateDNSRecords = (domain: Domain) => {
    return `# MX Record:
${domain.name}.    IN    MX    10    mail.${domain.name}.

# SPF Record:
${domain.name}.    IN    TXT    "v=spf1 mx ~all"

# DKIM Record:
default._domainkey.${domain.name}.    IN    TXT    "v=DKIM1; k=rsa; p=${domain.dkim_public_key?.replace(/\n/g, "") || "KEY"}"

# DMARC Record:
_dmarc.${domain.name}.    IN    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@${domain.name}"`;
  };

  const copyDNSToClipboard = (domain: Domain) => {
    navigator.clipboard.writeText(generateDNSRecords(domain));
    setCopiedDNS(true);
    setTimeout(() => setCopiedDNS(false), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Domains</h1>
          <p className="text-muted-foreground mt-1">
            Manage your email domains and DNS configuration
          </p>
        </div>
        <Dialog open={isAddDialogOpen} onOpenChange={setIsAddDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Domain
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Add New Domain</DialogTitle>
              <DialogDescription>
                Enter the domain name you want to add to your email server.
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
                <Label htmlFor="domain">Domain Name</Label>
                <Input
                  id="domain"
                  placeholder="example.com"
                  value={newDomainName}
                  onChange={(e) => setNewDomainName(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="max-accounts">Max Accounts</Label>
                <Input
                  id="max-accounts"
                  type="number"
                  value={newDomainMaxAccounts}
                  onChange={(e) => setNewDomainMaxAccounts(parseInt(e.target.value) || 100)}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setIsAddDialogOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleCreateDomain}>Add Domain</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {/* Search and Filter */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search domains..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-10"
          />
        </div>
        <Button
          variant="outline"
          size="icon"
          onClick={() => fetchDomains()}
          disabled={loading}
        >
          <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
        </Button>
      </div>

      {/* Domains List */}
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
      ) : filteredDomains?.length === 0 ? (
        <Card className="text-center py-12">
          <CardContent>
            <Globe className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="text-lg font-medium">No domains found</h3>
            <p className="text-muted-foreground mt-1">
              {searchQuery
                ? "No domains match your search"
                : "Get started by adding your first domain"}
            </p>
            {!searchQuery && (
              <Button className="mt-4" onClick={() => setIsAddDialogOpen(true)}>
                <Plus className="mr-2 h-4 w-4" />
                Add Domain
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {filteredDomains?.map((domain: Domain) => (
            <DomainCard
              key={domain.name}
              domain={domain}
              onToggle={() => handleToggleDomain(domain)}
              onDelete={() => {
                setSelectedDomain(domain);
                setIsDeleteDialogOpen(true);
              }}
              onCopyDNS={() => copyDNSToClipboard(domain)}
              copiedDNS={copiedDNS}
            />
          ))}
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      <Dialog open={isDeleteDialogOpen} onOpenChange={setIsDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Domain</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete {selectedDomain?.name}? This action cannot be
              undone and all associated accounts will be removed.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteDomain}>
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface DomainCardProps {
  domain: Domain;
  onToggle: () => void;
  onDelete: () => void;
  onCopyDNS: () => void;
  copiedDNS: boolean;
}

function DomainCard({ domain, onToggle, onDelete, onCopyDNS, copiedDNS }: DomainCardProps) {
  const [showDNS, setShowDNS] = useState(false);

  return (
    <Card className="group">
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-gradient-to-br from-blue-500 to-blue-600">
              <Globe className="h-5 w-5 text-white" />
            </div>
            <div>
              <CardTitle className="text-lg">{domain.name}</CardTitle>
              <CardDescription>
                {domain.is_active ? (
                  <span className="flex items-center gap-1 text-emerald-500">
                    <Shield className="h-3 w-3" />
                    Active
                  </span>
                ) : (
                  <span className="text-muted-foreground">Inactive</span>
                )}
              </CardDescription>
            </div>
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="h-8 w-8">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setShowDNS(true)}>
                <Mail className="mr-2 h-4 w-4" />
                View DNS Records
              </DropdownMenuItem>
              <DropdownMenuItem onClick={onCopyDNS}>
                {copiedDNS ? (
                  <Check className="mr-2 h-4 w-4" />
                ) : (
                  <Copy className="mr-2 h-4 w-4" />
                )}
                {copiedDNS ? "Copied!" : "Copy DNS Records"}
              </DropdownMenuItem>
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
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Max Accounts</span>
            <Badge variant="secondary">{domain.max_accounts}</Badge>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">Status</span>
            <div className="flex items-center gap-2">
              <Switch checked={domain.is_active} onCheckedChange={onToggle} />
            </div>
          </div>
          {domain.dkim_selector && (
            <div className="flex items-center gap-2 pt-2">
              <Shield className="h-4 w-4 text-emerald-500" />
              <span className="text-sm text-muted-foreground">DKIM Enabled</span>
            </div>
          )}
        </div>
      </CardContent>

      {/* DNS Records Dialog */}
      <Dialog open={showDNS} onOpenChange={setShowDNS}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>DNS Records for {domain.name}</DialogTitle>
            <DialogDescription>
              Add these records to your DNS provider to enable email delivery
            </DialogDescription>
          </DialogHeader>
          <div className="bg-muted rounded-lg p-4 font-mono text-sm overflow-x-auto">
            <pre>{`# MX Record:
${domain.name}.    IN    MX    10    mail.${domain.name}.

# SPF Record:
${domain.name}.    IN    TXT    "v=spf1 mx ~all"

# DMARC Record:
_dmarc.${domain.name}.    IN    TXT    "v=DMARC1; p=quarantine; rua=mailto:dmarc@${domain.name}"`}</pre>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => onCopyDNS()}>
              {copiedDNS ? (
                <>
                  <Check className="mr-2 h-4 w-4" />
                  Copied!
                </>
              ) : (
                <>
                  <Copy className="mr-2 h-4 w-4" />
                  Copy Records
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
