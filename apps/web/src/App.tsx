import { ThemeProvider } from "next-themes";
import { Toaster } from "@/components/ui/toaster";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes, useParams } from "react-router-dom";
import { AuthProvider } from "@/contexts/AuthContext";
import { ProtectedRoute } from "@/components/auth/ProtectedRoute";
import { RequireAdmin } from "@/components/auth/RequireAdmin";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ConfirmDialogProvider } from "@/components/ui/confirm-dialog";
import Audit from "./pages/Audit";
import { AppLayout } from "@/components/layout/AppLayout";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Pipelines from "./pages/Pipelines";
import Executions from "./pages/Executions";
import Schedules from "./pages/Schedules";
import ScheduleDetail from "./pages/ScheduleDetail";
import Policies from "./pages/Policies";
import Observability from "./pages/Observability";
import Settings from "./pages/Settings";
import ApiKeyCreate from "./pages/ApiKeyCreate";
import NotFound from "./pages/NotFound";
import { usePipelineWebSocket } from "@/hooks/use-pipeline-ws";

// There is one pipeline UI: the list with its side panel. This keeps older
// /pipelines/:id links working by sending them there with the run selected,
// rather than maintaining a second, thinner view of the same thing.
function PipelineRoute() {
  const { id } = useParams();
  return <Navigate to={`/pipelines?search=${id}&selected=${id}`} replace />;
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
                <Route path="/" element={<Dashboard />} />
                <Route path="/pipelines" element={<Pipelines />} />
                <Route path="/pipelines/:id" element={<PipelineRoute />} />
                <Route path="/executions" element={<Executions />} />
                <Route path="/schedules" element={<Schedules />} />
                <Route path="/schedules/:id" element={<ScheduleDetail />} />
                <Route path="/action-policies" element={<RequireAdmin what="action policies"><Policies /></RequireAdmin>} />
                <Route path="/policies" element={<Navigate to="/action-policies" replace />} />
                <Route path="/observability" element={<RequireAdmin what="observability"><Observability /></RequireAdmin>} />
                <Route path="/settings" element={<RequireAdmin what="settings"><Settings /></RequireAdmin>} />
                <Route path="/audit" element={<RequireAdmin what="the audit log"><Audit /></RequireAdmin>} />
              <Route path="/settings/api-key/new" element={<RequireAdmin what="settings"><ApiKeyCreate /></RequireAdmin>} />
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
