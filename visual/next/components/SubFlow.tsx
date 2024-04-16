import { memo } from 'react';
import { Handle, NodeProps, Position, NodeResizer  } from 'reactflow';
import { splitByFirstParenthesis } from './Step';

function SubFlow({
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
            <article className='text-wrap antialiased text-gray-700 text-center'>
                <h3 className='font-bold my-1'>{name}</h3><p className='line-clamp-3'>{para}</p>
                <hr/>
                <div className='h-full opacity-70'></div>
            </article>
            <Handle className='invisible' type='source' position={sourcePosition || Position.Right} isConnectable={isConnectable}></Handle>
        </div>
    );
}

export default memo(SubFlow);