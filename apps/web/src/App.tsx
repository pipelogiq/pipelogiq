import { Toaster } from "@/components/ui/toaster";
import { Toaster as Sonner } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider } from "@/contexts/AuthContext";
import { ProtectedRoute } from "@/components/auth/ProtectedRoute";
import { AppLayout } from "@/components/layout/AppLayout";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Pipelines from "./pages/Pipelines";
import PipelineDetail from "./pages/PipelineDetail";
import Executions from "./pages/Executions";
import Policies from "./pages/Policies";
import Observability from "./pages/Observability";
import Settings from "./pages/Settings";
import ApiKeyCreate from "./pages/ApiKeyCreate";
import NotFound from "./pages/NotFound";
import { usePipelineWebSocket } from "@/hooks/use-pipeline-ws";

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
  <QueryClientProvider client={queryClient}>
    <PipelineWebSocketProvider>
    <AuthProvider>
      <TooltipProvider>
        <Toaster />
        <Sonner />
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route
              element={
                <ProtectedRoute>
                  <AppLayout />
                </ProtectedRoute>
              }
            >
              <Route path="/" element={<Dashboard />} />
              <Route path="/pipelines" element={<Pipelines />} />
              <Route path="/pipelines/:id" element={<PipelineDetail />} />
              <Route path="/executions" element={<Executions />} />
              <Route path="/action-policies" element={<Policies />} />
              <Route path="/policies" element={<Navigate to="/action-policies" replace />} />
              <Route path="/observability" element={<Observability />} />
              <Route path="/settings" element={<Settings />} />
              <Route path="/settings/api-key/new" element={<ApiKeyCreate />} />
            </Route>
            <Route path="*" element={<NotFound />} />
          </Routes>
        </BrowserRouter>
      </TooltipProvider>
    </AuthProvider>
    </PipelineWebSocketProvider>
  </QueryClientProvider>
);

export default App;
