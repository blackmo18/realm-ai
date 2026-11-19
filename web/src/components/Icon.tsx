const paths: Record<string, string> = {
  realm: 'M12 2l9 5v10l-9 5-9-5V7z M12 22V12 M21 7l-9 5-9-5',
  plus: 'M12 5v14 M5 12h14',
  folder: 'M3 7h6l2 2h10v10H3z M12 12v5 M9.5 14.5h5',
  ask: 'M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20 M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3 M12 17h.01',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  warn: 'M12 3l10 18H2z M12 10v5 M12 18h.01',
  check: 'M5 12l5 5L20 7',
};

interface IconProps {
  name: keyof typeof paths;
  size?: number;
}

export function Icon({ name, size = 16 }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {paths[name].split(' M').map((d, i) => (
        <path key={i} d={i === 0 ? d : 'M' + d} />
      ))}
    </svg>
  );
}
