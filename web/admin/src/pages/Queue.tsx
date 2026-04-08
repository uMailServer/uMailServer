import { useState, useEffect } from "react";
import {
  Mail,
  RefreshCw,
  RotateCcw,
  Trash2,
  AlertCircle,
  CheckCircle,
  Clock,
  XCircle,
  Send,
  Filter,
  ChevronLeft,
  ChevronRight,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useQueue } from "@/hooks/useApi";
import { cn } from "@/lib/utils";
import type { QueueEntry } from "@/types";

export function Queue() {
  const { entries, loading, fetchQueue, retryEntry, dropEntry } = useQueue();
  const [filter, setFilter] = useState<string>("all");
  const [currentPage, setCurrentPage] = useState(1);
  const [selectedEntry, setSelectedEntry] = useState<QueueEntry | null>(null);
  const [isRetryDialogOpen, setIsRetryDialogOpen] = useState(false);
  const [isDropDialogOpen, setIsDropDialogOpen] = useState(false);

  const itemsPerPage = 10;

  useEffect(() => {
    fetchQueue();
    const interval = setInterval(fetchQueue, 10000);
    return () => clearInterval(interval);
  }, [fetchQueue]);

  const filteredEntries = entries?.filter((entry: QueueEntry) => {
    if (filter === "all") return true;
    return entry.status === filter;
  });

  const totalPages = Math.ceil((filteredEntries?.length || 0) / itemsPerPage);
  const paginatedEntries = filteredEntries?.slice(
    (currentPage - 1) * itemsPerPage,
    currentPage * itemsPerPage
  );

  const handleRetry = async () => {
    if (!selectedEntry) return;
    await retryEntry(selectedEntry.id);
    setIsRetryDialogOpen(false);
    setSelectedEntry(null);
  };

  const handleDrop = async () => {
    if (!selectedEntry) return;
    await dropEntry(selectedEntry.id);
    setIsDropDialogOpen(false);
    setSelectedEntry(null);
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "delivered":
        return <CheckCircle className="h-4 w-4 text-emerald-500" />;
      case "failed":
        return <XCircle className="h-4 w-4 text-red-500" />;
      case "sending":
        return <Send className="h-4 w-4 text-blue-500" />;
      case "pending":
        return <Clock className="h-4 w-4 text-amber-500" />;
      default:
        return <Mail className="h-4 w-4 text-gray-500" />;
    }
  };

  const getStatusBadge = (status: string) => {
    const variants: Record<string, string> = {
      delivered: "bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20",
      failed: "bg-red-500/10 text-red-500 hover:bg-red-500/20",
      sending: "bg-blue-500/10 text-blue-500 hover:bg-blue-500/20",
      pending: "bg-amber-500/10 text-amber-500 hover:bg-amber-500/20",
    };
    return variants[status] || "bg-gray-500/10 text-gray-500";
  };

  const stats = {
    total: entries?.length || 0,
    pending: entries?.filter((e: QueueEntry) => e.status === "pending").length || 0,
    sending: entries?.filter((e: QueueEntry) => e.status === "sending").length || 0,
    failed: entries?.filter((e: QueueEntry) => e.status === "failed").length || 0,
    delivered: entries?.filter((e: QueueEntry) => e.status === "delivered").length || 0,
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Email Queue</h1>
          <p className="text-muted-foreground mt-1">
            Monitor and manage outgoing email queue
          </p>
        </div>
        <Button variant="outline" onClick={fetchQueue} disabled={loading}>
          <RefreshCw className={cn("mr-2 h-4 w-4", loading && "animate-spin")} />
          Refresh
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-5">
        <StatCard title="Total" value={stats.total} icon={Mail} color="from-gray-500 to-gray-600" />
        <StatCard title="Pending" value={stats.pending} icon={Clock} color="from-amber-500 to-amber-600" />
        <StatCard title="Sending" value={stats.sending} icon={Send} color="from-blue-500 to-blue-600" />
        <StatCard title="Failed" value={stats.failed} icon={AlertCircle} color="from-red-500 to-red-600" />
        <StatCard title="Delivered" value={stats.delivered} icon={CheckCircle} color="from-emerald-500 to-emerald-600" />
      </div>

      {/* Filter and List */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle>Queue Entries</CardTitle>
              <CardDescription>
                {filteredEntries?.length || 0} messages in queue
              </CardDescription>
            </div>
            <Select value={filter} onValueChange={(value) => setFilter(value ?? "all")}>
              <SelectTrigger className="w-40">
                <Filter className="h-4 w-4 mr-2" />
                <SelectValue placeholder="Filter by status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Status</SelectItem>
                <SelectItem value="pending">Pending</SelectItem>
                <SelectItem value="sending">Sending</SelectItem>
                <SelectItem value="failed">Failed</SelectItem>
                <SelectItem value="delivered">Delivered</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="space-y-4">
              {[1, 2, 3, 4, 5].map((i) => (
                <Skeleton key={i} className="h-16 w-full" />
              ))}
            </div>
          ) : paginatedEntries?.length === 0 ? (
            <div className="text-center py-12">
              <Mail className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
              <h3 className="text-lg font-medium">No queue entries</h3>
              <p className="text-muted-foreground mt-1">
                The email queue is currently empty
              </p>
            </div>
          ) : (
            <>
              <div className="space-y-2">
                {paginatedEntries?.map((entry: QueueEntry) => (
                  <QueueItem
                    key={entry.id}
                    entry={entry}
                    onRetry={() => {
                      setSelectedEntry(entry);
                      setIsRetryDialogOpen(true);
                    }}
                    onDrop={() => {
                      setSelectedEntry(entry);
                      setIsDropDialogOpen(true);
                    }}
                    getStatusIcon={getStatusIcon}
                    getStatusBadge={getStatusBadge}
                  />
                ))}
              </div>

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-between mt-6 pt-4 border-t">
                  <p className="text-sm text-muted-foreground">
                    Page {currentPage} of {totalPages}
                  </p>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
                      disabled={currentPage === 1}
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setCurrentPage(p => Math.min(totalPages, p + 1))}
                      disabled={currentPage === totalPages}
                    >
                      <ChevronRight className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {/* Retry Dialog */}
      <Dialog open={isRetryDialogOpen} onOpenChange={setIsRetryDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Retry Email</DialogTitle>
            <DialogDescription>
              Are you sure you want to retry sending this email?
            </DialogDescription>
          </DialogHeader>
          {selectedEntry && (
            <div className="bg-muted p-4 rounded-lg text-sm">
              <p><strong>To:</strong> {selectedEntry.to}</p>
              <p><strong>From:</strong> {selectedEntry.from}</p>
              <p><strong>Status:</strong> {selectedEntry.status}</p>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsRetryDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleRetry}>
              <RotateCcw className="mr-2 h-4 w-4" />
              Retry
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Drop Dialog */}
      <Dialog open={isDropDialogOpen} onOpenChange={setIsDropDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove from Queue</DialogTitle>
            <DialogDescription>
              Are you sure you want to permanently remove this email from the queue?
            </DialogDescription>
          </DialogHeader>
          {selectedEntry && (
            <div className="bg-muted p-4 rounded-lg text-sm">
              <p><strong>To:</strong> {selectedEntry.to}</p>
              <p><strong>From:</strong> {selectedEntry.from}</p>
              <p><strong>Status:</strong> {selectedEntry.status}</p>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsDropDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDrop}>
              <Trash2 className="mr-2 h-4 w-4" />
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface StatCardProps {
  title: string;
  value: number;
  icon: React.ElementType;
  color: string;
}

function StatCard({ title, value, icon: Icon, color }: StatCardProps) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center gap-3">
          <div className={cn("p-2 rounded-lg bg-gradient-to-br", color)}>
            <Icon className="h-4 w-4 text-white" />
          </div>
          <div>
            <div className="text-2xl font-bold">{value}</div>
            <div className="text-xs text-muted-foreground">{title}</div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

interface QueueItemProps {
  entry: QueueEntry;
  onRetry: () => void;
  onDrop: () => void;
  getStatusIcon: (status: string) => React.ReactNode;
  getStatusBadge: (status: string) => string;
}

function QueueItem({ entry, onRetry, onDrop, getStatusIcon, getStatusBadge }: QueueItemProps) {
  return (
    <div className="flex items-center gap-4 p-4 rounded-lg border hover:bg-muted/50 transition-colors">
      <div className="flex-shrink-0">{getStatusIcon(entry.status)}</div>

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium truncate">{entry.to}</span>
          <Badge variant="secondary" className={cn("text-xs", getStatusBadge(entry.status))}>
            {entry.status}
          </Badge>
        </div>
        <div className="text-sm text-muted-foreground">
          From: {entry.from}
        </div>
        {entry.last_error && (
          <div className="text-xs text-red-500 mt-1 truncate">
            Error: {entry.last_error}
          </div>
        )}
      </div>

      <div className="text-xs text-muted-foreground hidden sm:block">
        {entry.retry_count > 0 && (
          <span className="block text-right">{entry.retry_count} retries</span>
        )}
        <span className="block text-right">
          {new Date(entry.created_at).toLocaleString()}
        </span>
      </div>

      <div className="flex items-center gap-2">
        {entry.status === "failed" && (
          <Button variant="ghost" size="sm" onClick={onRetry}>
            <RotateCcw className="h-4 w-4" />
          </Button>
        )}
        <Button variant="ghost" size="sm" onClick={onDrop}>
          <Trash2 className="h-4 w-4 text-red-500" />
        </Button>
      </div>
    </div>
  );
}
