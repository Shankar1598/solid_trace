import { NavLink, Outlet, useParams } from 'react-router-dom';
import { cn } from '../lib/utils';
import { Building2, User } from 'lucide-react';

export function SettingsLayout() {
  const { orgSlug } = useParams();

  const navItems = [
    {
      label: 'Organization',
      icon: Building2,
      to: `/${orgSlug}/settings/organization`,
    },
    {
      label: 'User',
      icon: User,
      to: `/${orgSlug}/settings/user`,
    },
  ];

  return (
    <div className="flex flex-col md:flex-row gap-8">
      <aside className="w-full md:w-64 flex-shrink-0">
        <div className="sticky top-6">
          <h2 className="text-lg font-semibold mb-4 px-3">Settings</h2>
          <nav className="space-y-1">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors",
                    isActive
                      ? "bg-secondary text-secondary-foreground"
                      : "text-muted-foreground hover:text-foreground hover:bg-secondary/50"
                  )
                }
              >
                <item.icon className="w-4 h-4" />
                {item.label}
              </NavLink>
            ))}
          </nav>
        </div>
      </aside>
      <main className="flex-1 min-w-0">
        <Outlet />
      </main>
    </div>
  );
}
