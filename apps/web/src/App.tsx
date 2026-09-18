import { ThemeProvider } from "next-themes";
import { Toaster } from "@/components/ui/toaster";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "@/contexts/AuthContext";
import { ProtectedRoute } from "@/components/auth/ProtectedRoute";
import { RequireAdmin } from "@/components/auth/RequireAdmin";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog";
import Audit from "./pages/Audit";
import { AppLayout } from "@/components/layout/AppLayout";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Pipelines from "./pages/Pipelines";
import PipelineDetail from "./pages/PipelineDetail";
import ExecutionsRedirect from "./pages/Executions";
import Schedules from "./pages/Schedules";
import ScheduleDetail from "./pages/ScheduleDetail";
import Policies from "./pages/Policies";
import Observability from "./pages/Observability";
import Settings, { LicenceSettings, SecuritySettings } from "./pages/Settings";
import { ApiKeysSettings } from "@/components/settings/ApiKeysSettings";
import Users from "./pages/Users";
import Account from "./pages/Account";
import ApiKeyCreate from "./pages/ApiKeyCreate";
import NotFound from "./pages/NotFound";
import { usePipelineWebSocket } from "@/hooks/use-pipeline-ws";

function HomeRoute() {
  const { isAdmin } = useAuth();
  return isAdmin ? <Dashboard /> : <Navigate to="/pipelines" replace />;
}

function PipelineWebSocketProvider({ children }: { children: React.ReactNode }) {
  usePipelineWebSocket();
  return <>{children}</>;
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const App = () => (
  // attribute="class" matches tailwind's darkMode: ["class"]; defaultTheme
  // "system" means an untouched install follows the operating system.
  <ErrorBoundary>
  <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <TooltipProvider>
          <Toaster />
        <ConfirmDialogProvider>
            <BrowserRouter>
            <Routes>
              <Route path="/login" element={<Login />} />
              <Route
                element={
                  <ProtectedRoute>
                    <PipelineWebSocketProvider>
                      <AppLayout />
                    </PipelineWebSocketProvider>
                  </ProtectedRoute>
                }
              >
                <Route path="/" element={<HomeRoute />} />
                <Route path="/pipelines" element={<Pipelines />} />
                <Route path="/pipelines/:id" element={<PipelineDetail />} />
                <Route path="/executions" element={<ExecutionsRedirect />} />
                <Route path="/schedules" element={<Schedules />} />
                <Route path="/schedules/:id" element={<ScheduleDetail />} />
                <Route path="/action-policies" element={<RequireAdmin what="action policies"><Policies /></RequireAdmin>} />
                <Route path="/policies" element={<Navigate to="/action-policies" replace />} />
                <Route path="/observability" element={<RequireAdmin what="observability"><Observability /></RequireAdmin>} />
                <Route path="/settings" element={<RequireAdmin what="settings"><Settings /></RequireAdmin>}>
                  <Route index element={<Navigate to="api-keys" replace />} />
                  <Route path="api-keys" element={<ApiKeysSettings />} />
                  <Route path="api-keys/new" element={<ApiKeyCreate />} />
                  <Route path="users" element={<Users />} />
                  <Route path="audit" element={<Audit />} />
                  <Route path="licence" element={<LicenceSettings />} />
                  <Route path="security" element={<SecuritySettings />} />
                  <Route path="api-key/new" element={<Navigate to="/settings/api-keys/new" replace />} />
                  <Route path="*" element={<Navigate to="/settings/api-keys" replace />} />
                </Route>
                <Route path="/users" element={<Navigate to="/settings/users" replace />} />
                <Route path="/account" element={<Account />} />
                <Route path="/audit" element={<Navigate to="/settings/audit" replace />} />
              </Route>
              <Route path="*" element={<NotFound />} />
            </Routes>
          </BrowserRouter>
        </ConfirmDialogProvider>
        </TooltipProvider>
      </AuthProvider>
    </QueryClientProvider>
  </ThemeProvider>
  </ErrorBoundary>
);

export default App;
