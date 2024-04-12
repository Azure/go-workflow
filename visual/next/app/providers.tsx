'use client';

import React from 'react';
import { NextUIProvider } from '@nextui-org/react';

export function Providers({ children }: { children: React.ReactNode }) {
    return <NextUIProvider style={{ height: '100%' }}>{children}</NextUIProvider>
}