import { useEffect } from "react";
import {
  Mail,
  Users,
  Globe,
  Server,
  TrendingUp,
  TrendingDown,
  Activity,
  CheckCircle,
  AlertTriangle,
  XCircle,
  Clock,
  RefreshCw,
} from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useStats } from "@/hooks/useApi";
import type { Activity as ActivityType, ServiceStatus, RealtimeMetrics } from "@/types";

interface DashboardProps {
  isConnected: boolean;
  metrics?: RealtimeMetrics;
  activities: ActivityType[];
}

const serviceStatuses: ServiceStatus[] = [
  { name: "SMTP Server", status: "operational", port: 25 },
  { name: "IMAP Server", status: "operational", port: 993 },
  { name: "HTTP API", status: "operational", port: 8443 },
];

export function Dashboard({ isConnected, metrics, activities }: DashboardProps) {
  const { stats, loading, fetchStats } = useStats();

  useEffect(() => {
    fetchStats();
    const interval = setInterval(fetchStats, 30000);
    return () => clearInterval(interval);
  }, [fetchStats]);

  const statCards = [
    {
      title: "Total Domains",
      value: stats?.domains || 0,
      icon: Globe,
      color: "from-blue-500 to-blue-600",
      trend: "+0",
      trendUp: true,
      description: "Active email domains",
    },
    {
      title: "Total Accounts",
      value: stats?.accounts || 0,
      icon: Users,
      color: "from-emerald-500 to-emerald-600",
      trend: "+0",
      trendUp: true,
      description: "Registered users",
    },
    {
      title: "Messages Today",
      value: stats?.messages || 0,
      icon: Mail,
      color: "from-violet-500 to-violet-600",
      trend: "+0",
      trendUp: true,
      description: "Processed messages",
    },
    {
      title: "Queue Size",
      value: stats?.queue_size || 0,
      icon: Server,
      color: "from-orange-500 to-orange-600",
      trend: "0",
      trendUp: true,
      description: "Pending emails",
    },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground mt-1">
            Overview of your email server performance and activity
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => fetchStats()}
          disabled={loading}
        >
          <RefreshCw className={cn("mr-2 h-4 w-4", loading && "animate-spin")} />
          Refresh
        </Button>
      </div>

      {/* Stats Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card, index) => (
          <StatCard key={index} {...card} loading={loading} />
        ))}
      </div>

      {/* Main Content Grid */}
      <div className="grid gap-6 lg:grid-cols-7">
        {/* System Status */}
        <Card className="lg:col-span-4">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-5 w-5" />
              System Status
            </CardTitle>
            <CardDescription>
              Real-time health of your email services
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 sm:grid-cols-3">
              {serviceStatuses.map((service) => (
                <ServiceCard key={service.name} {...service} />
              ))}
            </div>

            {metrics && (
              <>
                <Separator className="my-6" />
                <div className="space-y-4">
                  <h4 className="text-sm font-medium">Resource Usage</h4>
                  <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                    <ResourceBar
                      label="CPU"
                      value={metrics.cpu_usage}
                      color="from-blue-500 to-blue-600"
                    />
                    <ResourceBar
                      label="Memory"
                      value={metrics.memory_usage}
                      color="from-emerald-500 to-emerald-600"
                    />
                    <ResourceBar
                      label="Disk"
                      value={metrics.disk_usage}
                      color="from-violet-500 to-violet-600"
                    />
                    <ResourceBar
                      label="Connections"
                      value={metrics.smtp_connections + metrics.imap_connections}
                      max={100}
                      color="from-orange-500 to-orange-600"
                    />
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Recent Activity */}
        <Card className="lg:col-span-3">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-5 w-5" />
              Recent Activity
              {isConnected && (
                <Badge variant="secondary" className="text-xs">
                  Live
                </Badge>
              )}
            </CardTitle>
            <CardDescription>Latest events from your server</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {activities.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  <Activity className="h-8 w-8 mx-auto mb-2 opacity-50" />
                  <p>No recent activity</p>
                </div>
              ) : (
                activities.slice(0, 10).map((activity) => (
                  <ActivityItem key={activity.id} {...activity} />
                ))
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

interface StatCardProps {
  title: string;
  value: number;
  icon: React.ElementType;
  color: string;
  trend: string;
  trendUp: boolean;
  description: string;
  loading?: boolean;
}

function StatCard({ title, value, icon: Icon, color, trend, trendUp, description, loading }: StatCardProps) {
  if (loading) {
    return (
      <Card>
        <CardContent className="p-6">
          <Skeleton className="h-12 w-12 rounded-lg mb-4" />
          <Skeleton className="h-8 w-24 mb-2" />
          <Skeleton className="h-4 w-32" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="relative overflow-hidden group hover:shadow-lg transition-shadow">
      <div className={cn("absolute top-0 right-0 w-32 h-32 bg-gradient-to-br opacity-10 rounded-bl-full", color)} />
      <CardContent className="p-6">
        <div className="flex items-start justify-between">
          <div className={cn("p-3 rounded-xl bg-gradient-to-br", color)}>
            <Icon className="h-6 w-6 text-white" />
          </div>
          <div className={cn(
            "flex items-center text-sm font-medium",
            trendUp ? "text-emerald-500" : "text-red-500"
          )}>
            {trendUp ? (
              <TrendingUp className="mr-1 h-4 w-4" />
            ) : (
              <TrendingDown className="mr-1 h-4 w-4" />
            )}
            {trend}
          </div>
        </div>
        <div className="mt-4">
          <div className="text-3xl font-bold">{value.toLocaleString()}</div>
          <div className="text-sm font-medium text-muted-foreground">{title}</div>
          <div className="text-xs text-muted-foreground mt-1">{description}</div>
        </div>
      </CardContent>
    </Card>
  );
}

function ServiceCard({ name, status, port }: ServiceStatus) {
  const statusConfig = {
    operational: { icon: CheckCircle, color: "text-emerald-500", bg: "bg-emerald-500/10" },
    degraded: { icon: AlertTriangle, color: "text-amber-500", bg: "bg-amber-500/10" },
    down: { icon: XCircle, color: "text-red-500", bg: "bg-red-500/10" },
  };

  const config = statusConfig[status];
  const Icon = config.icon;

  return (
    <div className="flex items-center gap-3 p-4 rounded-lg bg-muted/50">
      <div className={cn("p-2 rounded-lg", config.bg)}>
        <Icon className={cn("h-5 w-5", config.color)} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="font-medium truncate">{name}</div>
        <div className="text-xs text-muted-foreground">Port {port}</div>
      </div>
      <Badge
        variant={status === "operational" ? "default" : "secondary"}
        className={cn(
          "text-xs",
          status === "operational" && "bg-emerald-500 hover:bg-emerald-600"
        )}
      >
        {status}
      </Badge>
    </div>
  );
}

interface ResourceBarProps {
  label: string;
  value: number;
  max?: number;
  color: string;
}

function ResourceBar({ label, value, max = 100, color }: ResourceBarProps) {
  const percentage = Math.min((value / max) * 100, 100);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-sm">
        <span className="font-medium">{label}</span>
        <span className="text-muted-foreground">{value}{max === 100 && "%"}</span>
      </div>
      <div className="h-2 rounded-full bg-muted overflow-hidden">
        <div
          className={cn("h-full rounded-full bg-gradient-to-r transition-all duration-500", color)}
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  );
}

function ActivityItem({ message, details, timestamp, severity = "info" }: ActivityType) {
  const severityConfig = {
    info: { icon: Activity, color: "text-blue-500", bg: "bg-blue-500/10" },
    success: { icon: CheckCircle, color: "text-emerald-500", bg: "bg-emerald-500/10" },
    warning: { icon: AlertTriangle, color: "text-amber-500", bg: "bg-amber-500/10" },
    error: { icon: XCircle, color: "text-red-500", bg: "bg-red-500/10" },
  };

  const config = severityConfig[severity];
  const Icon = config.icon;

  return (
    <div className="flex items-start gap-3 p-3 rounded-lg hover:bg-muted/50 transition-colors">
      <div className={cn("p-2 rounded-lg flex-shrink-0", config.bg)}>
        <Icon className={cn("h-4 w-4", config.color)} />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium">{message}</p>
        {details && <p className="text-xs text-muted-foreground mt-0.5">{details}</p>}
        <p className="text-xs text-muted-foreground mt-1">
          {new Date(timestamp).toLocaleTimeString()}
        </p>
      </div>
    </div>
  );
}
