interface TagProps {
  label: string;
  color?: string;
  size?: 'sm' | 'md';
}

export default function Tag({ label, color = '#6366f1', size = 'sm' }: TagProps) {
  const sizeClasses = size === 'sm' ? 'text-xs px-2 py-0.5' : 'text-sm px-2.5 py-1';

  return (
    <span
      className={`inline-flex items-center rounded-full font-medium ${sizeClasses}`}
      style={{
        backgroundColor: `${color}20`,
        color: color,
      }}
    >
      {label}
    </span>
  );
}
