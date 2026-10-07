import { Module } from '@nestjs/common';
import { DeploymentsService } from './deployments.service';
import { ProjectsController } from './projects.controller';
import { ProjectsService } from './projects.service';

@Module({
  controllers: [ProjectsController],
  providers: [ProjectsService, DeploymentsService],
})
export class ProjectsModule {}
