import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
  type ReactNode,
} from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { authApi, ApiError, SESSION_EXPIRED_EVENT } from '@/api/client';
import type { User, LoginRequest } from '@/types/api';

interface AuthContextType {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  /** Admin, Administrator and SuperAdmin roles, case-insensitively. */
  isAdmin: boolean;
  /** Administrators and operators can change pipeline and schedule state. */
  canOperate: boolean;
  login: (data: LoginRequest) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const currentUser = useRef<User | null>(null);
  // Ignore an auth check that completes after logout or session expiration.
  const sessionVersion = useRef(0);
  const latestAuthCheck = useRef(0);

  const setSession = useCallback((next: User | null) => {
    if (!next || currentUser.current?.id !== next.id || currentUser.current?.role !== next.role) {
      queryClient.clear();
    }
    currentUser.current = next;
    setUser(next);
  }, [queryClient]);

  const checkAuth = useCallback(() => {
    const version = sessionVersion.current;
    const check = ++latestAuthCheck.current;
    const isCurrent = () => version === sessionVersion.current && check === latestAuthCheck.current;
    return authApi.getCurrentUser().then(userData => {
      if (isCurrent()) setSession(userData);
    }).catch(error => {
      if (!(error instanceof ApiError && error.status === 401)) {
        console.error('Auth check failed:', error);
      }
      if (isCurrent()) setSession(null);
    }).finally(() => {
      if (isCurrent()) setIsLoading(false);
    });
  }, [setSession]);

  useEffect(() => {
    checkAuth();
  }, [checkAuth]);

  useEffect(() => {
    const onExpired = () => {
      sessionVersion.current += 1;
      setSession(null);
      setIsLoading(false);
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, onExpired);
    return () => window.removeEventListener(SESSION_EXPIRED_EVENT, onExpired);
  }, [setSession]);

  const login = async (data: LoginRequest) => {
    const version = ++sessionVersion.current;
    setSession(null);
    setIsLoading(true);
    try {
      await authApi.login(data);
      if (version === sessionVersion.current) await checkAuth();
    } finally {
      if (version === sessionVersion.current) setIsLoading(false);
    }
  };

  const logout = async () => {
    const version = ++sessionVersion.current;
    setSession(null);
    setIsLoading(true);
    try {
      await authApi.logout();
    } finally {
      if (version === sessionVersion.current) setIsLoading(false);
    }
  };

  const normalizedRole = (user?.role ?? '').trim().toLowerCase();
  const isAdmin = ['admin', 'administrator', 'superadmin'].includes(normalizedRole);

  return (
    <AuthContext.Provider
      value={{
        user,
        isLoading,
        isAuthenticated: !!user,
        isAdmin,
        canOperate: isAdmin || ['operator', 'user'].includes(normalizedRole),
        login,
        logout,
        checkAuth,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
