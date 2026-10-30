import { useState, useEffect, useRef } from "react";
import { AtSign, Plus, RefreshCw, Trash2, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useAliases } from "@/hooks/useApi";
import { emailError } from "@/lib/validation";
import { cn } from "@/lib/utils";
import type { Alias } from "@/types";

export function Aliases() {
  const { aliases, loading, fetchAliases, createAlias, updateAlias, deleteAlias } = useAliases();

  const [alias, setAlias] = useState("");
  const [target, setTarget] = useState("");
  const [error, setError] = useState("");
  // Row whose delete is awaiting confirmation.
  const [confirming, setConfirming] = useState<string | null>(null);
  // Guards every mutation against a double submit (ref: synchronous).
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const guarded = async (fn: () => Promise<void>) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      await fn();
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  useEffect(() => {
    fetchAliases().catch(() => undefined);
  }, [fetchAliases]);

  const message = (err: unknown, fallback: string) => (err instanceof Error ? err.message : fallback);

  const handleCreate = () =>
    guarded(async () => {
      setError("");
      const a = alias.trim();
      const t = target.trim();
      if (!a || !t) {
        setError("Alias and target address are required");
        return;
      }
      const invalid = emailError(a) ?? emailError(t);
      if (invalid) {
        setError(invalid);
        return;
      }
      try {
        await createAlias(a, t);
        setAlias("");
        setTarget("");
      } catch (err) {
        setError(message(err, "Failed to create alias"));
      }
    });

  const handleToggle = (row: Alias, active: boolean) =>
    guarded(async () => {
      setError("");
      try {
        await updateAlias(row.alias, { is_active: active });
      } catch (err) {
        setError(message(err, "Failed to update alias"));
      }
    });

  const handleDelete = (row: Alias) =>
    guarded(async () => {
      setError("");
      try {
        await deleteAlias(row.alias);
        setConfirming(null);
      } catch (err) {
        setError(message(err, "Failed to delete alias"));
      }
    });

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Aliases</h1>
          <p className="text-muted-foreground mt-1">
            Forward an address to an existing account
          </p>
        </div>
        <Button
          variant="outline"
          size="icon"
          aria-label="Refresh aliases"
          onClick={() => fetchAliases().catch(() => undefined)}
          disabled={loading}
        >
          <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
        </Button>
      </div>

      {error && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Add alias</CardTitle>
          <CardDescription>Mail sent to the alias is delivered to the target account.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
            <div className="space-y-2 flex-1">
              <Label htmlFor="alias-address">Alias address</Label>
              <Input
                id="alias-address"
                type="email"
                placeholder="info@example.com"
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
              />
            </div>
            <div className="space-y-2 flex-1">
              <Label htmlFor="alias-target">Target account</Label>
              <Input
                id="alias-target"
                type="email"
                placeholder="user@example.com"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              />
            </div>
            <Button onClick={handleCreate} disabled={busy}>
              <Plus className="mr-2 h-4 w-4" />
              Add Alias
            </Button>
          </div>
        </CardContent>
      </Card>

      {loading && !aliases ? (
        <Skeleton className="h-24 w-full" />
      ) : !aliases || aliases.length === 0 ? (
        <Card className="text-center py-12">
          <CardContent>
            <AtSign className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <h3 className="text-lg font-medium">No aliases yet</h3>
            <p className="text-muted-foreground mt-1">Add the first one above.</p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Alias</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {aliases.map((row) => (
                  <TableRow key={row.alias}>
                    <TableCell className="font-medium">{row.alias}</TableCell>
                    <TableCell>{row.target}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Switch
                          aria-label={`Toggle ${row.alias}`}
                          checked={row.is_active}
                          disabled={busy}
                          onCheckedChange={(v) => handleToggle(row, v)}
                        />
                        <Badge variant={row.is_active ? "default" : "secondary"}>
                          {row.is_active ? "Active" : "Disabled"}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {confirming === row.alias ? (
                        <div className="flex justify-end gap-2">
                          <Button
                            size="sm"
                            variant="destructive"
                            disabled={busy}
                            onClick={() => handleDelete(row)}
                          >
                            Confirm delete
                          </Button>
                          <Button size="sm" variant="outline" onClick={() => setConfirming(null)}>
                            Cancel
                          </Button>
                        </div>
                      ) : (
                        <Button
                          size="sm"
                          variant="ghost"
                          aria-label={`Delete ${row.alias}`}
                          onClick={() => setConfirming(row.alias)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
