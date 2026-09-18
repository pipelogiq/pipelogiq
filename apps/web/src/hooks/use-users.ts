import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { usersApi } from '@/api/client';
import type { UpdateUserRequest } from '@/types/users';

export function useUsers() {
  return useQuery({ queryKey: ['users'], queryFn: usersApi.getAll });
}

export function useUserApplications() {
  return useQuery({ queryKey: ['user-applications'], queryFn: usersApi.getApplications });
}

export function useUserRoles() {
  return useQuery({ queryKey: ['user-roles'], queryFn: usersApi.getRoles, staleTime: Infinity });
}

export function useCreateUser() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: usersApi.create,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
  });
}

export function useUpdateUser() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateUserRequest }) => usersApi.update(id, data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
  });
}

export function useResetUserPassword() {
  return useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) => usersApi.resetPassword(id, password),
  });
}
