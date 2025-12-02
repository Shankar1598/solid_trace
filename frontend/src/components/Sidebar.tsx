import { Link, useLocation, useParams } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import {
  LayoutDashboard,
  AlertCircle,
  Settings,
  LogOut,
  User,
  ChevronsUpDown,
  Plus
} from 'lucide-react';
import { cn } from '../lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export function Sidebar() {
  const { user, logout } = useAuth();
  const location = useLocation();
  const { orgSlug } = useParams();

  if (!user) return null;

  const currentOrg = user.organizations.find(o => o.slug === orgSlug) || user.organizations[0];

  const navItems = [
    {
      label: 'Issues',
      icon: AlertCircle,
      href: `/${currentOrg?.slug}/issues`,
      active: location.pathname.includes('/issues')
    },
    {
      label: 'Projects',
      icon: LayoutDashboard,
      href: `/${currentOrg?.slug}/projects`, // Placeholder route
      active: location.pathname.includes('/projects')
    },
    {
      label: 'Settings',
      icon: Settings,
      href: `/${currentOrg?.slug}/settings`, // Placeholder route
      active: location.pathname.includes('/settings')
    }
  ];

  return (
    <div className="w-64 bg-[#2b1c3d] text-white flex flex-col h-screen border-r border-gray-800">
      {/* Org Switcher */}
      <div className="p-4 border-b border-white/10">
        <DropdownMenu>
          <DropdownMenuTrigger className="w-full flex items-center justify-between hover:bg-white/10 p-2 rounded transition-colors outline-none">
            <div className="flex items-center gap-2 font-semibold">
              <div className="w-8 h-8 bg-white/10 rounded flex items-center justify-center">
                {currentOrg?.name.charAt(0).toUpperCase()}
              </div>
              <span className="truncate">{currentOrg?.name}</span>
            </div>
            <ChevronsUpDown className="w-4 h-4 opacity-50" />
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56 bg-[#2b1c3d] text-white border-gray-700">
            <DropdownMenuLabel>Organizations</DropdownMenuLabel>
            {user.organizations.map(org => (
              <DropdownMenuItem key={org.id} className="focus:bg-white/10 focus:text-white cursor-pointer">
                <Link to={`/${org.slug}/issues`} className="w-full">
                  {org.name}
                </Link>
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator className="bg-white/10" />
            <DropdownMenuItem className="focus:bg-white/10 focus:text-white cursor-pointer">
              <Plus className="w-4 h-4 mr-2" />
              Create Organization
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-4 space-y-1">
        {navItems.map((item) => (
          <Link
            key={item.label}
            to={item.href}
            className={cn(
              "flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors",
              item.active
                ? "bg-white/10 text-white"
                : "text-gray-400 hover:text-white hover:bg-white/5"
            )}
          >
            <item.icon className="w-4 h-4" />
            {item.label}
          </Link>
        ))}
      </nav>

      {/* User Profile */}
      <div className="p-4 border-t border-white/10">
        <DropdownMenu>
          <DropdownMenuTrigger className="w-full flex items-center gap-3 hover:bg-white/10 p-2 rounded transition-colors outline-none">
            <div className="w-8 h-8 bg-purple-500 rounded-full flex items-center justify-center text-sm font-bold">
              {user.email.charAt(0).toUpperCase()}
            </div>
            <div className="flex-1 text-left overflow-hidden">
              <p className="text-sm font-medium truncate">{user.email}</p>
            </div>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56 bg-[#2b1c3d] text-white border-gray-700">
            <DropdownMenuItem className="focus:bg-white/10 focus:text-white cursor-pointer">
              <User className="w-4 h-4 mr-2" />
              Profile
            </DropdownMenuItem>
            <DropdownMenuSeparator className="bg-white/10" />
            <DropdownMenuItem onClick={logout} className="focus:bg-white/10 focus:text-white cursor-pointer text-red-400 focus:text-red-400">
              <LogOut className="w-4 h-4 mr-2" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
