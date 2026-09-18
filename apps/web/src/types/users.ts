export type UserRole = 'admin' | 'operator' | 'viewer';

export interface ManagedUser {
  id: number;
  firstName: string;
  lastName?: string;
  email: string;
  role: UserRole;
  createdAt: string;
  isActive: boolean;
  applicationIds: number[];
}

export interface UserApplication {
  id: number;
  name: string;
  description?: string;
}

export interface UserRoleDefinition {
  id: UserRole;
  name: string;
  description: string;
  permissions: string[];
}

export interface UpdateUserRequest {
  firstName: string;
  lastName?: string;
  email: string;
  role: UserRole;
  isActive: boolean;
  applicationIds: number[];
}

export interface CreateUserRequest extends UpdateUserRequest {
  password: string;
}
