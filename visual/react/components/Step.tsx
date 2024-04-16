import { memo } from 'react';
import { Handle, NodeProps, Position, NodeResizer  } from 'reactflow';

export function splitByFirstParenthesis(label: string): { name: string, para: string } {
    const index = label.indexOf('(') || 0;
    return {
        name: index !== -1 ? label.substring(0, index) : label,
        para: index !== -1 ? label.substring(index) : '',
    }
}

function Step({
    data,
    targetPosition,
    sourcePosition,
    isConnectable,
    selected,
}: NodeProps) {
    const { name, para } = splitByFirstParenthesis(data?.label || '');

    return (
        <div className='h-full'>
            <NodeResizer color='#94a3b8' isVisible={selected}/>
            <Handle className='invisible' type='target' position={targetPosition || Position.Left} isConnectable={isConnectable}></Handle>
            <article className='text-clip whitespace-normal break-all text-wrap antialiased text-gray-700 text-center h-full'>
                <h3 className='font-bold my-1'>{name}</h3><p className='line-clamp-3 h-full'>{para}</p>
            </article>
            <Handle className='invisible' type='source' position={sourcePosition || Position.Right} isConnectable={isConnectable}></Handle>
        </div>
    );
}

export default memo(Step);